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
	ErrOrder = errors.New("out of order")
	ErrRange = errors.New("out of range")
)

type RingPos int64

// RingBuffer is a io.Writer that uses a single byte buffer to store data written to it
// until Release is called on the range no-longer required. RingBuffer is effectively a
// linear allocator with sequential frees that must be done in the same order as the
// allocations.
type RingBuffer struct {
	rwlock sync.RWMutex

	data []byte

	readIndex  RingPos
	writeIndex RingPos
}

var _ io.Writer = (*RingBuffer)(nil)

// NewRingBuffer creates a RingBuffer with the provided size in bytes for the backing
// buffer.
func NewRingBuffer(size int) *RingBuffer {
	rb := RingBuffer{
		data: make([]byte, size),
	}
	return &rb
}

// Write writes p to the backing buffer, allocating the number of bytes in p.
// If p is larger than the amount of space available in the buffer then
// io.ErrShortWrite is returned and the number of bytes written.
func (rb *RingBuffer) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	rb.rwlock.Lock()
	defer rb.rwlock.Unlock()
	available := rb.available()
	writeLength := len(p)
	if writeLength > available {
		writeLength = available
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

// Free number of bytes available to allocate.
func (rb *RingBuffer) Available() int {
	rb.rwlock.RLock()
	defer rb.rwlock.RUnlock()
	return rb.available()
}

func (rb *RingBuffer) available() int {
	return len(rb.data) - int(rb.writeIndex-rb.readIndex)
}

// of the internal buffer.
func (rb *RingBuffer) Size() int {
	return len(rb.data)
}

// Pos of current write index.
func (rb *RingBuffer) Pos() RingPos {
	rb.rwlock.RLock()
	defer rb.rwlock.RUnlock()
	return rb.writeIndex
}

// Copy bytes from the range into the supplied dest buffer. If dest is not large enough
// to fill the bytes from start to end, then start to start+len(dest) is copied and
// the error io.ErrShortBuffer is returned.
func (rb *RingBuffer) Copy(dest []byte, start RingPos, end RingPos) (int, error) {
	rb.rwlock.RLock()
	defer rb.rwlock.RUnlock()
	if end < start {
		return 0, ErrRange
	}
	if start < rb.readIndex || start > rb.writeIndex {
		return 0, ErrRange
	}
	if end < rb.readIndex || end > rb.writeIndex {
		return 0, ErrRange
	}
	copyLength := int(end - start)
	if copyLength > len(dest) {
		copyLength = len(dest)
	}
	low := int(start % RingPos(len(rb.data)))
	high := int((start + RingPos(copyLength)) % RingPos(len(rb.data)))
	if high == 0 {
		high = len(rb.data)
	}
	n := 0
	if low < high {
		n = copy(dest, rb.data[low:high])
	} else {
		lowLength := len(rb.data) - low
		n = copy(dest[:lowLength], rb.data[low:])
		n += copy(dest[lowLength:], rb.data[:high])
	}
	if n < int(end-start) {
		return n, io.ErrShortBuffer
	}
	return n, nil
}

// WriteTo writes the selected range to a io.Writer.
func (rb *RingBuffer) WriteTo(writer io.Writer, start RingPos, end RingPos) (int64, error) {
	rb.rwlock.RLock()
	defer rb.rwlock.RUnlock()
	if end < start {
		return 0, ErrRange
	}
	if start < rb.readIndex || start > rb.writeIndex {
		return 0, ErrRange
	}
	if end < rb.readIndex || end > rb.writeIndex {
		return 0, ErrRange
	}
	copyLength := int(end - start)
	low := int(start % RingPos(len(rb.data)))
	high := int((start + RingPos(copyLength)) % RingPos(len(rb.data)))
	if high == 0 {
		high = len(rb.data)
	}
	if low < high {
		n, err := writer.Write(rb.data[low:high])
		return int64(n), err
	}
	n0, err := writer.Write(rb.data[low:])
	if err != nil {
		return int64(n0), err
	}
	n1, err := writer.Write(rb.data[:high])
	return int64(n0 + n1), err
}

// Discard releases a range of the RingBuffer so that it may be reused. Start must be the
// earliest allocated position. End must be up to the latest allocated position or
// any value in between.
func (rb *RingBuffer) Discard(start, end RingPos) error {
	rb.rwlock.Lock()
	defer rb.rwlock.Unlock()
	if end < start {
		return ErrRange
	}
	if start < rb.readIndex || start > rb.writeIndex {
		return ErrRange
	}
	if end < rb.readIndex || end > rb.writeIndex {
		return ErrRange
	}
	if start != rb.readIndex {
		return ErrOrder
	}
	rb.readIndex = end
	return nil
}

// Buffers for the selected range. Use after Release is undefined.
func (rb *RingBuffer) Buffers(start, end RingPos) [2][]byte {
	rb.rwlock.Lock()
	defer rb.rwlock.Unlock()
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
	if start != rb.readIndex {
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
