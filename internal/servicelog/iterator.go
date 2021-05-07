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
	"sync/atomic"
	"time"
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
	BufferedWrite
}

type BufferedWrite interface {
	// Reset resets the read position in the current buffered write, so that
	// Read and WriteTo can re-read the whole buffered write.
	Reset()
	// Buffered returns how many bytes are remaining to be read in the current
	// buffered write.
	Buffered() int
	// Timestamp returns the time the current buffered write was recorded.
	Timestamp() time.Time
	// StreamID returns the StreamID of the current buffered write.
	// This can be either Stdout or Stderr.
	StreamID() StreamID
	io.Reader
	io.WriterTo
}

type iterator struct {
	wb         *WriteBuffer
	index      int32
	skip       bool
	read       int
	notifyChan chan struct{}
	closeChan  chan struct{}
}

var _ Iterator = (*iterator)(nil)

func (it *iterator) Close() error {
	if it.wb == nil {
		return nil
	}
	it.wb.removeIterator(it)
	close(it.notifyChan)
	it.wb = nil
	return nil
}

func (it *iterator) Next(cancel <-chan struct{}) bool {
	if it.wb == nil {
		return false
	}
	if it.skip {
		// already have the first write.
		it.skip = false
		return true
	}
	select {
	case <-it.notifyChan:
	default:
	}
	ok := it.wb.advanceIterator(it)
	if !ok && cancel != nil {
		// if passed a cancel channel, wait for a buffered write.
		select {
		case <-it.closeChan:
			return false
		case <-cancel:
			return false
		case <-it.notifyChan:
			ok = it.wb.advanceIterator(it)
		}
	}
	if !ok {
		return false
	}
	return true
}

func (it *iterator) storeIndex(next WriteIndex) {
	atomic.StoreInt32(&it.index, int32(next))
	it.read = 0
}

func (it *iterator) readIndex() WriteIndex {
	return WriteIndex(atomic.LoadInt32(&it.index))
}

func (it *iterator) Reset() {
	it.read = 0
}

// Read implements io.Reader
func (it *iterator) Read(dest []byte) (int, error) {
	if it.wb == nil {
		return 0, io.EOF
	}
	write := it.write()
	if write == nil {
		return 0, io.EOF
	}
	start := write.start + RingPos(it.read)
	end := write.end
	if start == end {
		return 0, io.EOF
	}
	read, err := it.wb.ringBuffer.Copy(dest, start, end)
	it.read += read
	if err != nil && err != io.ErrShortBuffer {
		return 0, err
	}
	return read, err
}

// WriteTo implements io.WriterTo
func (it *iterator) WriteTo(writer io.Writer) (int64, error) {
	if it.wb == nil {
		return 0, io.EOF
	}
	write := it.write()
	if write == nil {
		return 0, io.EOF
	}
	start := write.start + RingPos(it.read)
	end := write.end
	read, err := it.wb.ringBuffer.WriteTo(writer, start, end)
	it.read += int(read)
	return read, err
}

func (it *iterator) Buffered() int {
	write := it.write()
	if write == nil {
		return 0
	}
	return write.length() - it.read
}

func (it *iterator) Timestamp() time.Time {
	write := it.write()
	if write == nil {
		return time.Time{}
	}
	return write.time
}

func (it *iterator) StreamID() StreamID {
	write := it.write()
	if write == nil {
		return Unknown
	}
	return write.streamID
}

func (it *iterator) write() *write {
	if it.wb == nil {
		return nil
	}
	index := WriteIndex(atomic.LoadInt32(&it.index))
	return it.wb.getWrite(index)
}
