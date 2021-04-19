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
	"time"
)

type Iterator interface {
	Close() error
	Next() bool
	More() <-chan struct{}
	Reset()
	BufferedWrite
}

type BufferedWrite interface {
	Length() int
	Timestamp() time.Time
	StreamID() StreamID
	Buffers() [2][]byte
	io.Reader
	io.WriterTo
}

type iterator struct {
	wb    *WriteBuffer
	write *write
	skip  bool
	read  int
}

var _ Iterator = (*iterator)(nil)

func (it *iterator) Close() error {
	if it.write != nil {
		it.write.release()
		it.write = nil
	}
	it.wb = nil
	return nil
}

func (it *iterator) Next() bool {
	if it.wb == nil {
		return false
	}
	if it.write == nil {
		return false
	}
	if it.skip {
		// already have the first write.
		it.skip = false
		return true
	}
	next, ok := it.wb.nextWrite(it.write)
	if !ok {
		return false
	}
	it.write = next
	it.read = 0
	return true
}

func (it *iterator) Reset() {
	it.read = 0
}

func (it *iterator) Read(dest []byte) (int, error) {
	if it.wb == nil {
		return 0, io.EOF
	}
	if it.write == nil || it.write.index == TailIndex {
		return 0, io.EOF
	}
	start := it.write.start + RingPos(it.read)
	end := it.write.end
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

func (it *iterator) WriteTo(writer io.Writer) (int64, error) {
	if it.wb == nil {
		return 0, io.EOF
	}
	if it.write == nil || it.write.index == TailIndex {
		return 0, io.EOF
	}
	start := it.write.start + RingPos(it.read)
	end := it.write.end
	read, err := it.wb.ringBuffer.WriteTo(writer, start, end)
	it.read += int(read)
	return read, err
}

func (it *iterator) Buffers() [2][]byte {
	if it.wb == nil {
		return [2][]byte{}
	}
	if it.write == nil {
		return [2][]byte{}
	}
	return it.wb.ringBuffer.Buffers(it.write.start, it.write.end)
}

func (it *iterator) Length() int {
	if it.write == nil {
		return 0
	}
	return it.write.length()
}

func (it *iterator) Timestamp() time.Time {
	if it.write == nil {
		return time.Time{}
	}
	return it.write.time
}

func (it *iterator) StreamID() StreamID {
	if it.write == nil {
		return Unknown
	}
	return it.write.streamID
}

func (it *iterator) More() <-chan struct{} {
	if it.wb == nil {
		return nil
	}
	return it.wb.more()
}
