// Copyright (c) 2021 Canonical Ltd
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License version 3 as
// published by the Free Software Foundation.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>

package servicelog

import (
	"fmt"
	"io"
	"sync"
)

type Iterator interface {
	// Close closes the iterator so that buffers can be used for future writes.
	// If Close is not called, the iterator will block buffer recycling causing
	// write failures.
	Close() error
	// Next moves the ring buffer read mark forward, making its tail available for reuse
	// without truncation. If the ring buffer writer produces data faster than the iterator
	// can read it, the iterator will eventually be truncated and restarted. The truncation
	// will be identified in the iterator output with the text specified when the iterator was
	// created.
	Next(cancel <-chan struct{}) bool

	// Notify sets the notification channel. When more data is available, the channel
	// passed in to Notify will have true sent on it. If the channel is not receiving (unbuffered)
	// or full (buffered), the notification will be dropped.
	Notify(ch chan bool)

	Buffered() int
	io.Reader
	io.WriterTo
}

type iterator struct {
	rb           *RingBuffer
	index        RingPos
	trunc        []byte
	truncWritten bool
	nextChan     chan bool
	closeChan    chan struct{}

	notifyLock sync.Mutex
	notifyChan chan bool
}

var _ Iterator = (*iterator)(nil)

var (
	truncBytes = []byte("\n(... output truncated ...)\n")
)

func (it *iterator) Close() error {
	if it.rb == nil {
		return nil
	}
	it.rb.removeIterator(it)
	close(it.nextChan)
	it.rb = nil
	return nil
}

func (it *iterator) Next(cancel <-chan struct{}) bool {
	if it.rb == nil {
		return false
	}
	select {
	case <-it.nextChan:
	default:
	}
	start, end := it.rb.Positions()
	if it.index != TailPosition && it.index < start {
		it.index = start
		it.truncated()
	}
	if end != 0 && it.index < end {
		return true
	}
	if len(it.trunc) > 0 {
		return true
	}
	for cancel != nil {
		// if passed a cancel channel, wait for more data.
		closed := false
		fmt.Printf("selecting inside cancel loop %p\n", it)
		select {
		case <-it.closeChan:
			closed = it.rb.Closed()
		case <-cancel:
			cancel = nil
		case <-it.nextChan:
		}
		fmt.Printf("....done selecting inside cancel loop %p\n", it)
		start, end := it.rb.Positions()
		data := make([]byte, 20)   // DEBUG
		it.rb.Copy(data, it.index) // DEBUG
		fmt.Printf("...iter %p (index=%v,start=%v,end=%v) data: %s\n", it, it.index, start, end, data)
		if it.index != TailPosition && it.index < start {
			it.index = start
			it.truncated()
		}
		if end != 0 && it.index < end {
			return true
		}
		if len(it.trunc) > 0 {
			return true
		}
		if it.index == end && closed {
			cancel = nil
		}
	}
	return false
}

// Read implements io.Reader
func (it *iterator) Read(dest []byte) (int, error) {
	if it.rb == nil {
		return 0, io.EOF
	}
	if len(it.trunc) > 0 {
		n := copy(dest, it.trunc)
		it.trunc = it.trunc[n:]
		it.truncWritten = true
		return n, nil
	}
	next, n, err := it.rb.Copy(dest, it.index)
	if n > 0 {
		it.truncWritten = false
	}
	it.index = next
	if err == ErrRange {
		it.truncated()
		return n, io.EOF
	}
	return n, err
}

// WriteTo implements io.WriterTo
func (it *iterator) WriteTo(writer io.Writer) (int64, error) {
	if it.rb == nil {
		return 0, io.EOF
	}
	if len(it.trunc) > 0 {
		n, err := writer.Write(it.trunc)
		it.trunc = it.trunc[n:]
		it.truncWritten = true
		return int64(n), err
	}
	next, n, err := it.rb.WriteTo(writer, it.index)
	if n > 0 {
		it.truncWritten = false
	}
	it.index = next
	if err == ErrRange {
		it.truncated()
		return n, io.EOF
	}
	return n, err
}

func (it *iterator) Buffered() int {
	start, end := it.rb.Positions()
	if it.index > start {
		start = it.index
	}
	return int(end - start)
}

func (it *iterator) Notify(ch chan bool) {
	it.notifyLock.Lock()
	defer it.notifyLock.Unlock()
	it.notifyChan = ch
}

func (it *iterator) truncated() {
	it.index = TailPosition
	if len(it.trunc) > 0 {
		// trunc being written
		return
	}
	if it.truncWritten {
		// trunc already written
		return
	}
	it.trunc = truncBytes
}
