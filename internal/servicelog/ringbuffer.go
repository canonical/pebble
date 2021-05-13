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
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package servicelog

import (
	"errors"
	"io"
	"sync"
)

var (
	ErrRange = errors.New("out of range")
)

type RingPos int64

// RingBuffer is a io.Writer that uses a single byte buffer to store data written to it
// until Release is called on the range no-longer required. RingBuffer is effectively a
// linear allocator with sequential frees that must be done in the same order as the
// allocations.
type RingBuffer struct {
	rwlock      sync.RWMutex
	readIndex   RingPos
	writeIndex  RingPos
	writeClosed bool
	data        []byte

	iteratorMutex sync.RWMutex
	iteratorList  []*iterator
}

var _ io.WriteCloser = (*RingBuffer)(nil)

// NewRingBuffer creates a RingBuffer with the provided size in bytes for the backing
// buffer.
func NewRingBuffer(size int) *RingBuffer {
	rb := RingBuffer{
		data: make([]byte, size),
	}
	return &rb
}

// Close closes the writer to further writes, readers may continue.
func (rb *RingBuffer) Close() error {
	rb.rwlock.Lock()
	defer rb.rwlock.Unlock()
	if rb.writeClosed {
		return nil
	}
	rb.writeClosed = true
	rb.releaseIterators()
	return nil
}

// Closed returns true if the writing side has closed.
func (rb *RingBuffer) Closed() bool {
	rb.rwlock.RLock()
	defer rb.rwlock.RUnlock()
	return rb.writeClosed
}

// Write writes p to the backing buffer, allocating the number of bytes in p.
// If p is larger than the size of the buffer then io.ErrShortWrite is returned and the
// number of bytes written. If the p is larger than the number of bytes available,
// then the tail is discarded to make room.
func (rb *RingBuffer) Write(p []byte) (written int, _ error) {
	if len(p) == 0 {
		return 0, nil
	}
	defer func() {
		if written > 0 {
			rb.signalIterators()
		}
	}()
	rb.rwlock.Lock()
	defer rb.rwlock.Unlock()
	if rb.writeClosed {
		return 0, io.ErrClosedPipe
	}
	size := rb.Size()
	writeLength := len(p)
	if writeLength > size {
		writeLength = size
	}
	available := rb.available()
	if available < writeLength {
		err := rb.discard(writeLength - available)
		if err != nil {
			return 0, err
		}
	}
	start := rb.writeIndex
	end := rb.writeIndex + RingPos(writeLength)
	low := int(start % RingPos(len(rb.data)))
	high := int(end % RingPos(len(rb.data)))
	if high == 0 {
		high = len(rb.data)
	}
	if low < high {
		copy(rb.data[low:high], p[0:writeLength])
	} else {
		lowLength := len(rb.data) - low
		copy(rb.data[low:], p)
		copy(rb.data[:high], p[lowLength:])
	}
	rb.writeIndex += RingPos(writeLength)
	if writeLength < len(p) {
		return writeLength, io.ErrShortWrite
	}
	return writeLength, nil
}

// Available returns the number of bytes available to allocate.
func (rb *RingBuffer) Available() int {
	rb.rwlock.RLock()
	defer rb.rwlock.RUnlock()
	return rb.available()
}

func (rb *RingBuffer) available() int {
	return len(rb.data) - int(rb.writeIndex-rb.readIndex)
}

// Buffered returns the number of bytes readable from the buffer.
func (rb *RingBuffer) Buffered() int {
	rb.rwlock.RLock()
	defer rb.rwlock.RUnlock()
	return rb.buffered()
}

func (rb *RingBuffer) buffered() int {
	return int(rb.writeIndex - rb.readIndex)
}

// Size returns the size in bytes of the internal buffer.
func (rb *RingBuffer) Size() int {
	return len(rb.data)
}

// Positions returns the start and end positions of readable data in the RingBuffer.
func (rb *RingBuffer) Positions() (start RingPos, end RingPos) {
	rb.rwlock.RLock()
	defer rb.rwlock.RUnlock()
	return rb.readIndex, rb.writeIndex
}

// Copy copies bytes from the range into the supplied dest buffer. If dest is not large enough
// to fill the bytes from start to end, then start to start+len(dest) is copied and
// the error io.ErrShortBuffer is returned.
func (rb *RingBuffer) Copy(dest []byte, start RingPos) (int, error) {
	rb.rwlock.RLock()
	defer rb.rwlock.RUnlock()
	if start < rb.readIndex || start > rb.writeIndex {
		return 0, ErrRange
	}
	if rb.writeClosed && start == rb.writeIndex {
		return 0, io.EOF
	}
	copyLength := int(rb.writeIndex - start)
	if copyLength > len(dest) {
		copyLength = len(dest)
	}
	if copyLength == 0 {
		return 0, nil
	}
	end := start + RingPos(copyLength)
	buffers := rb.buffers(start, end)
	written := 0
	for _, buffer := range buffers {
		if len(buffer) == 0 {
			continue
		}
		n := copy(dest, buffer)
		dest = dest[n:]
		written += n
	}
	if rb.writeClosed && start+RingPos(written) == rb.writeIndex {
		return written, io.EOF
	}
	return written, nil
}

// WriteTo writes the selected range to a io.Writer.
func (rb *RingBuffer) WriteTo(writer io.Writer, start RingPos) (int64, error) {
	rb.rwlock.RLock()
	defer rb.rwlock.RUnlock()
	if start < rb.readIndex || start > rb.writeIndex {
		return 0, ErrRange
	}
	if rb.writeClosed && start == rb.writeIndex {
		return 0, io.EOF
	}
	copyLength := rb.writeIndex - start
	if copyLength == 0 {
		return 0, nil
	}
	end := rb.writeIndex
	buffers := rb.buffers(start, end)
	written := int64(0)
	for _, buffer := range buffers {
		if len(buffer) == 0 {
			continue
		}
		n, err := writer.Write(buffer)
		written += int64(n)
		if err != nil {
			return written, err
		}
	}
	if rb.writeClosed && start+RingPos(written) == rb.writeIndex {
		return written, io.EOF
	}
	return written, nil
}

// TailIterator returns an iterator from the tail of the buffer.
func (rb *RingBuffer) TailIterator() Iterator {
	rb.iteratorMutex.Lock()
	defer rb.iteratorMutex.Unlock()
	start, _ := rb.Positions()
	iter := &iterator{
		rb:         rb,
		index:      start,
		notifyChan: make(chan struct{}),
		closeChan:  make(chan struct{}),
	}
	if rb.Closed() {
		close(iter.closeChan)
	}
	rb.iteratorList = append(rb.iteratorList, iter)
	return iter
}

// HeadIterator returns an iterator from the head of the buffer.
func (rb *RingBuffer) HeadIterator(lines int) Iterator {
	firstLine := rb.reverseLinePosition(lines)
	rb.iteratorMutex.Lock()
	defer rb.iteratorMutex.Unlock()
	iter := &iterator{
		rb:         rb,
		index:      firstLine,
		notifyChan: make(chan struct{}),
		closeChan:  make(chan struct{}),
	}
	if rb.Closed() {
		close(iter.closeChan)
	}
	rb.iteratorList = append(rb.iteratorList, iter)
	return iter
}

func (rb *RingBuffer) reverseLinePosition(n int) RingPos {
	rb.rwlock.RLock()
	defer rb.rwlock.RUnlock()
	if n <= 0 {
		return rb.writeIndex
	}
	buffers := rb.buffers(rb.readIndex, rb.writeIndex)
	// a line is not complete until newline is written, so start negative.
	lines := -1
	firstLine := rb.writeIndex
	last := byte(0)
out:
	for j := len(buffers) - 1; j >= 0; j-- {
		buf := buffers[j]
		for i := len(buf) - 1; i >= 0; i-- {
			firstLine--
			last = buf[i]
			if last == byte('\n') {
				lines++
			}
			if lines == n {
				break out
			}
		}
	}
	if last == byte('\n') {
		firstLine++
	}
	return firstLine
}

// Discard disposes of n bytes from the tail of the buffer making
// them available to be used for subsequent writes.
func (rb *RingBuffer) Discard(n int) error {
	rb.rwlock.Lock()
	defer rb.rwlock.Unlock()
	return rb.discard(n)
}

func (rb *RingBuffer) discard(n int) error {
	buffered := rb.buffered()
	if n > buffered {
		n = buffered
	}
	rb.readIndex = rb.readIndex + RingPos(n)
	return nil
}

func (rb *RingBuffer) signalIterators() {
	rb.iteratorMutex.RLock()
	defer rb.iteratorMutex.RUnlock()
	for _, iter := range rb.iteratorList {
		select {
		case iter.notifyChan <- struct{}{}:
		default:
		}
	}
}

func (rb *RingBuffer) releaseIterators() {
	rb.iteratorMutex.Lock()
	defer rb.iteratorMutex.Unlock()
	for _, iter := range rb.iteratorList {
		close(iter.closeChan)
	}
	rb.iteratorList = nil
}

func (rb *RingBuffer) removeIterator(iter *iterator) {
	rb.iteratorMutex.Lock()
	defer rb.iteratorMutex.Unlock()
	for i, storedIter := range rb.iteratorList {
		if iter != storedIter {
			continue
		}
		close(iter.closeChan)
		rb.iteratorList[i] = rb.iteratorList[len(rb.iteratorList)-1]
		rb.iteratorList = rb.iteratorList[:len(rb.iteratorList)-1]
		return
	}
}

func (rb *RingBuffer) buffers(start, end RingPos) [2][]byte {
	buffers := [2][]byte{}
	if end < start {
		return buffers
	}
	if start < rb.readIndex || start > rb.writeIndex {
		return buffers
	}
	if end < rb.readIndex || end > rb.writeIndex {
		return buffers
	}
	low := int(start % RingPos(len(rb.data)))
	high := int(end % RingPos(len(rb.data)))
	if high == 0 {
		high = len(rb.data)
	}
	if low < high {
		buffers[0] = rb.data[low:high]
		return buffers
	}
	buffers[0] = rb.data[low:]
	buffers[1] = rb.data[:high]
	return buffers
}
