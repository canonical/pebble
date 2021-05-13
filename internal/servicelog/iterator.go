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
	"io"
)

type Iterator interface {
	// Close closes the iterator so that buffers can be used for future writes.
	// If Close is not called, the iterator will block buffer recycling causing
	// write failures.
	Close() error
	// Next advances to the next available buffered write.
	// When passed cancel, Next will wait for the next buffered write.
	// If a nil channel is passed, cancel will return the first available
	// buffered write, if none is available, return immediatley.
	Next(cancel <-chan struct{}) bool

	Buffered() int
	io.Reader
	io.WriterTo
}

type iterator struct {
	rb           *RingBuffer
	index        RingPos
	trunc        []byte
	truncWritten bool
	notifyChan   chan struct{}
	closeChan    chan struct{}
}

var _ Iterator = (*iterator)(nil)

var (
	truncBytes = []byte("<trunc>\n")
)

func (it *iterator) Close() error {
	if it.rb == nil {
		return nil
	}
	it.rb.removeIterator(it)
	close(it.notifyChan)
	it.rb = nil
	return nil
}

func (it *iterator) Next(cancel <-chan struct{}) bool {
	if it.rb == nil {
		return false
	}
	select {
	case <-it.notifyChan:
	default:
	}
	start, end := it.rb.Positions()
	if it.index < start {
		it.index = start
		it.truncated()
	}
	if it.index < end || len(it.trunc) > 0 {
		return true
	}
	for cancel != nil {
		// if passed a cancel channel, wait for more data.
		closed := false
		select {
		case <-it.closeChan:
			closed = it.rb.Closed()
		case <-cancel:
			cancel = nil
		case <-it.notifyChan:
		}
		start, end := it.rb.Positions()
		if it.index < start {
			it.index = start
			it.truncated()
		}
		if it.index < end || len(it.trunc) > 0 {
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
	n, err := it.rb.Copy(dest, it.index)
	if err == ErrRange {
		it.truncated()
		err = nil
	}
	if n > 0 {
		it.truncWritten = false
	}
	it.index += RingPos(n)
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
	n, err := it.rb.WriteTo(writer, it.index)
	if err == ErrRange {
		it.truncated()
		err = nil
	}
	if n > 0 {
		it.truncWritten = false
	}
	it.index += RingPos(n)
	return n, err
}

func (it *iterator) Buffered() int {
	start, end := it.rb.Positions()
	if it.index > start {
		start = it.index
	}
	return int(end - start)
}

func (it *iterator) truncated() {
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
