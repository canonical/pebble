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
	"sync"
)

type Iterator interface {
	// Close removes this iterator from the ring buffer. After calling Close,
	// any future calls to Next will return false.
	Close() error

	// Next returns true if there is more data to read. If a non-nil cancel
	// channel is passed in, Next will wait for more data to become available.
	// Sending on this channel, or closing it, will cause Next to return
	// immediately.
	// If the ring buffer writer produces data faster than the iterator can read
	// it, the iterator will eventually be truncated and restarted. The
	// truncation will be identified in the iterator output with the text
	// specified when the iterator was created.
	Next(cancel <-chan struct{}) bool

	// Notify sets the notification channel. When more data is available, the
	// channel passed in to Notify will have true sent on it. If the channel is
	// not receiving (unbuffered) or full (buffered), the notification will be
	// dropped.
	Notify(ch chan bool)

	// Buffered returns the approximate number of bytes available to read.
	Buffered() int

	// Reverse switches the iterator to read backwards one line at a time,
	// starting with the line before the current position. While reversed,
	// Read and WriteTo read no further than the end of the current line
	// (Read returns io.EOF there), and Next never waits for more data. Once
	// the current line has been fully read, the following call to Next moves
	// the position to the start of that line and returns true if there is a
	// previous line to read. A line is only treated as complete if it starts
	// after a newline, or at the very start of the log.
	Reverse()

	// Forward switches the iterator to reading forwards (the default) from the
	// current position, which is the start of the last line Next moved past
	// while reversed. So a line that has been read while reversed, but that
	// Next hasn't moved past, is not read again.
	Forward()

	io.Reader
	io.WriterTo
}

type iterator struct {
	mu           sync.Mutex
	rb           *RingBuffer
	index        RingPos
	trunc        []byte
	truncWritten bool
	nextChan     chan bool
	closeChan    chan struct{}

	notifyLock sync.Mutex
	notifyChan chan bool

	// Reverse line iteration state, used when reverse is true.
	reverse   bool
	hasLine   bool    // whether there is a current line
	lineFirst RingPos // position of the first character of the current line
	lineLast  RingPos // position of the last character of the current line ('\n')
	lineIndex RingPos // position of the next byte to read in the current line
}

var _ Iterator = (*iterator)(nil)

var (
	truncBytes = []byte("\n(... output truncated ...)\n")
)

func (it *iterator) Close() error {
	it.mu.Lock()
	rb := it.rb
	if rb == nil {
		it.mu.Unlock()
		return nil
	}
	it.rb = nil
	it.mu.Unlock()

	rb.iteratorMutex.Lock()
	defer rb.iteratorMutex.Unlock()
	rb.removeIterator(it)
	close(it.nextChan)
	return nil
}

func (it *iterator) Next(cancel <-chan struct{}) bool {
	it.mu.Lock()
	rb := it.rb
	it.mu.Unlock()
	if rb == nil {
		return false
	}
	if it.reverse {
		return it.previousLine(rb)
	}
	select {
	case <-it.nextChan:
	default:
	}
	start, end := rb.Positions()
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
		select {
		case <-it.closeChan:
			it.mu.Lock()
			rb = it.rb
			it.mu.Unlock()
			if rb == nil {
				return false
			}
			closed = rb.Closed()
		case <-cancel:
			cancel = nil
		case <-it.nextChan:
		}
		it.mu.Lock()
		rb = it.rb
		it.mu.Unlock()
		if rb == nil {
			return false
		}
		start, end := rb.Positions()
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
	it.mu.Lock()
	rb := it.rb
	it.mu.Unlock()
	if rb == nil {
		return 0, io.EOF
	}
	if it.reverse {
		return it.readLine(rb, dest)
	}
	if len(it.trunc) > 0 {
		n := copy(dest, it.trunc)
		it.trunc = it.trunc[n:]
		it.truncWritten = true
		return n, nil
	}
	next, n, err := rb.Copy(dest, it.index)
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
	it.mu.Lock()
	rb := it.rb
	it.mu.Unlock()
	if rb == nil {
		return 0, io.EOF
	}
	if it.reverse {
		return it.writeLine(rb, writer)
	}
	if len(it.trunc) > 0 {
		n, err := writer.Write(it.trunc)
		it.trunc = it.trunc[n:]
		it.truncWritten = true
		return int64(n), err
	}
	next, n, err := rb.WriteTo(writer, it.index)
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
	it.mu.Lock()
	rb := it.rb
	it.mu.Unlock()
	if rb == nil {
		return 0
	}
	start, end := rb.Positions()
	if it.reverse {
		if !it.hasLine || it.lineIndex < start {
			return 0
		}
		return int(it.lineLast + 1 - it.lineIndex)
	}
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

func (it *iterator) Reverse() {
	it.mu.Lock()
	rb := it.rb
	it.mu.Unlock()
	if rb == nil {
		return
	}
	rb.rwlock.RLock()
	defer rb.rwlock.RUnlock()
	it.reverse = true
	it.trunc = nil
	it.truncWritten = false
	if it.index == TailPosition || it.index < rb.readIndex {
		it.index = rb.readIndex
	}
	it.setLine(rb, it.index)
}

func (it *iterator) Forward() {
	it.reverse = false
	it.hasLine = false
}

// previousLine implements Next when reversed.
func (it *iterator) previousLine(rb *RingBuffer) bool {
	rb.rwlock.RLock()
	defer rb.rwlock.RUnlock()
	if it.hasLine && it.lineIndex > it.lineLast {
		// The current line has been read, so move to the line before it.
		it.index = it.lineFirst
		it.setLine(rb, it.index)
	}
	return it.hasLine && it.lineIndex >= rb.readIndex
}

// readLine implements Read when reversed.
func (it *iterator) readLine(rb *RingBuffer, dest []byte) (int, error) {
	if !it.hasLine || it.lineIndex > it.lineLast {
		return 0, io.EOF
	}
	if remaining := int(it.lineLast + 1 - it.lineIndex); len(dest) > remaining {
		dest = dest[:remaining]
	}
	next, n, err := rb.Copy(dest, it.lineIndex)
	if err == ErrRange {
		// The rest of the line has been discarded.
		it.hasLine = false
		return 0, io.EOF
	}
	it.lineIndex = next
	if it.lineIndex > it.lineLast {
		return n, io.EOF
	}
	return n, nil
}

// writeLine implements WriteTo when reversed.
func (it *iterator) writeLine(rb *RingBuffer, writer io.Writer) (int64, error) {
	if !it.hasLine || it.lineIndex > it.lineLast {
		return 0, nil
	}
	rb.rwlock.RLock()
	defer rb.rwlock.RUnlock()
	if it.lineIndex < rb.readIndex {
		// The rest of the line has been discarded.
		it.hasLine = false
		return 0, io.EOF
	}
	next, n, err := rb.writeRange(writer, it.lineIndex, it.lineLast+1)
	it.lineIndex = next
	return n, err
}

// setLine sets the current line to the line ending just before end. The
// caller must hold rb.rwlock (read).
func (it *iterator) setLine(rb *RingBuffer, end RingPos) {
	it.hasLine = false
	if end <= rb.readIndex {
		return
	}
	first, ok := rb.lineStart(end - 1)
	if !ok {
		// The start of the line has been discarded.
		return
	}
	it.hasLine = true
	it.lineFirst = first
	it.lineLast = end - 1
	it.lineIndex = first
}
