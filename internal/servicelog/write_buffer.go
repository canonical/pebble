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
	"sync/atomic"
	"time"
)

type StreamID int

var (
	Unknown StreamID = 0
	Stdout  StreamID = 1
	Stderr  StreamID = 2
)

func (s StreamID) String() string {
	switch s {
	case Stdout:
		return "stdout"
	case Stderr:
		return "stderr"
	default:
		return "unknown"
	}
}

type WriteIndex int32

var (
	TailIndex WriteIndex = -1
)

type write struct {
	time       time.Time
	start, end RingPos
	streamID   StreamID
	index      WriteIndex
	ref        int32
}

func (l *write) empty() bool {
	return l.time == time.Time{} && l.start == RingPos(0) && l.end == RingPos(0)
}

func (l *write) length() int {
	return int(l.end - l.start)
}

func (l *write) acquire() int {
	return int(atomic.AddInt32(&l.ref, 1))
}

func (l *write) release() int {
	return int(atomic.AddInt32(&l.ref, -1))
}

func (l *write) refs() int {
	return int(atomic.LoadInt32(&l.ref))
}

type WriteBuffer struct {
	ringBuffer      *RingBuffer
	writes          []write
	placeholderTail write

	writeMutex   sync.Mutex
	writeClosed  bool
	releaseMutex sync.RWMutex
	signalMut    sync.RWMutex
	signalChan   chan struct{}

	tailIndex int32
	headIndex int32
	numWrites int32
}

var (
	ErrNoMoreLines = errors.New("no more lines")
)

func NewWriteBuffer(maxWrites, maxBytes int) *WriteBuffer {
	wb := WriteBuffer{
		writes:     make([]write, maxWrites),
		ringBuffer: NewRingBuffer(maxBytes),
		tailIndex:  0,
		headIndex:  -1,
		numWrites:  0,
		signalChan: make(chan struct{}),
		placeholderTail: write{
			index: TailIndex,
		},
	}
	return &wb
}

func (wb *WriteBuffer) Close() error {
	wb.writeMutex.Lock()
	defer wb.writeMutex.Unlock()
	if wb.writeClosed {
		return nil
	}
	wb.writeClosed = true
	wb.releaseWaiters()
	return nil
}

func (wb *WriteBuffer) StreamWriter(streamID StreamID) io.Writer {
	return &streamWriter{wb, streamID}
}

func (wb *WriteBuffer) Write(p []byte, streamID StreamID) (int, error) {
	written := 0
	defer func() {
		if written > 0 {
			wb.signalWaiters()
		}
	}()

	writeTime := time.Now().UTC()

	wb.writeMutex.Lock()
	defer wb.writeMutex.Unlock()

	if wb.writeClosed {
		return 0, io.ErrClosedPipe
	}

	writeLength := len(p)
	if writeLength > wb.ringBuffer.Capacity() {
		writeLength = wb.ringBuffer.Capacity()
	}

	for writeLength > wb.ringBuffer.Free() || !wb.canAdvanceHead() {
		err := wb.releaseTail()
		if err != nil {
			return written, err
		}
	}

	index := WriteIndex(atomic.LoadInt32(&wb.headIndex) + 1)
	write := wb.getWrite(index)
	write.index = index
	write.time = writeTime
	write.streamID = streamID
	write.start = wb.ringBuffer.Pos()
	n, err := wb.ringBuffer.Write(p[:writeLength])
	if err != nil && err != io.ErrShortWrite {
		return 0, err
	}
	write.end = wb.ringBuffer.Pos()
	written = n

	wb.advanceHead()

	if written < len(p) {
		return written, io.ErrShortWrite
	}

	return written, nil
}

// TailIterator returns an iterator from the tail of the stream.
// The caller must Close the iterator when finished.
func (wb *WriteBuffer) TailIterator() Iterator {
	tail := wb.acquireTail()
	return &iterator{
		wb:    wb,
		write: tail,
		skip:  tail != &wb.placeholderTail,
	}
}

// HeadIterator returns an iterator from the head of the stream.
// If last is a positive non-zero number, the iterator will start
// at most N writes back from the head.
// The caller must Close the iterator when finished.
func (wb *WriteBuffer) HeadIterator(last int) Iterator {
	head := wb.acquireHead(last)
	skip := head != &wb.placeholderTail
	if last < 1 {
		// Since the head is the last write in the buffer
		// and the caller didn't ask for the last line, tell
		// the iterator to not skip the first Next call.
		skip = false
	}
	return &iterator{
		wb:    wb,
		write: head,
		skip:  skip,
	}
}

func (wb *WriteBuffer) getWrite(index WriteIndex) *write {
	idx := int(index) % len(wb.writes)
	return &wb.writes[idx]
}

func (wb *WriteBuffer) releaseTail() error {
	wb.releaseMutex.Lock()
	defer wb.releaseMutex.Unlock()
	numWrites := atomic.LoadInt32(&wb.numWrites)
	if numWrites == 0 {
		return ErrNoMoreLines
	}
	for wb.placeholderTail.refs() > 0 {
		time.Sleep(100 * time.Microsecond)
	}
	tailIndex := WriteIndex(atomic.LoadInt32(&wb.tailIndex))
	l := wb.getWrite(tailIndex)
	atomic.AddInt32(&wb.tailIndex, 1)
	for l.refs() > 0 {
		time.Sleep(100 * time.Microsecond)
	}
	if !l.empty() {
		err := wb.ringBuffer.Release(l.start, l.end)
		if err != nil {
			// Release should not ever fail, but restore gracefully.
			atomic.AddInt32(&wb.tailIndex, -1)
			return err
		}
		*l = write{}
	}
	atomic.AddInt32(&wb.numWrites, -1)
	return nil
}

func (wb *WriteBuffer) canAdvanceHead() bool {
	numWrites := atomic.LoadInt32(&wb.numWrites)
	return int(numWrites) < len(wb.writes)
}

func (wb *WriteBuffer) advanceHead() {
	atomic.AddInt32(&wb.numWrites, 1)
	atomic.AddInt32(&wb.headIndex, 1)
}

func (wb *WriteBuffer) signalWaiters() {
	wb.signalMut.Lock()
	defer wb.signalMut.Unlock()
	close(wb.signalChan)
	wb.signalChan = make(chan struct{})
}

func (wb *WriteBuffer) more() <-chan struct{} {
	wb.signalMut.RLock()
	defer wb.signalMut.RUnlock()
	return wb.signalChan
}

func (wb *WriteBuffer) releaseWaiters() {
	wb.signalMut.Lock()
	defer wb.signalMut.Unlock()
	close(wb.signalChan)
}

// acquireTail returns the tail and acquires a lock on it.
// the caller must either release it or call nextWrite to
// exchange it for the subsequent write.
func (wb *WriteBuffer) acquireTail() *write {
	wb.releaseMutex.RLock()
	defer wb.releaseMutex.RUnlock()
	numWrites := atomic.LoadInt32(&wb.numWrites)
	if numWrites == 0 {
		write := &wb.placeholderTail
		write.acquire()
		return write
	}
	tailIndex := WriteIndex(atomic.LoadInt32(&wb.tailIndex))
	write := wb.getWrite(tailIndex)
	write.acquire()
	return write
}

// acquireHead returns the tail and acquires a lock on it.
// the caller must either release it or call nextWrite to
// exchange it for the subsequent write.
func (wb *WriteBuffer) acquireHead(last int) *write {
	wb.releaseMutex.RLock()
	defer wb.releaseMutex.RUnlock()
	numWrites := atomic.LoadInt32(&wb.numWrites)
	if numWrites == 0 {
		write := &wb.placeholderTail
		write.acquire()
		return write
	}
	if last < 0 {
		last = 0
	}
	if last > int(numWrites) {
		last = int(numWrites)
	}
	headIndex := WriteIndex(atomic.LoadInt32(&wb.headIndex))
	if last > 0 {
		headIndex -= WriteIndex(last - 1)
	}
	write := wb.getWrite(headIndex)
	write.acquire()
	return write
}

// nextWrite returns the next write in the sequence or false.
// if false is returned, the caller is still responsible for releasing
// the current write. If true is returned the current write was
// released by nextWrite and a new write was returned.
func (wb *WriteBuffer) nextWrite(current *write) (*write, bool) {
	if current == &wb.placeholderTail {
		numWrites := atomic.LoadInt32(&wb.numWrites)
		if numWrites == 0 {
			return nil, false
		}
		nextIndex := WriteIndex(atomic.LoadInt32(&wb.tailIndex))
		next := wb.getWrite(nextIndex)
		next.acquire()
		current.release()
		return next, true
	}
	nextIndex := current.index + 1
	headIndex := WriteIndex(atomic.LoadInt32(&wb.headIndex))
	if nextIndex > headIndex {
		return nil, false
	}
	next := wb.getWrite(nextIndex)
	next.acquire()
	current.release()
	return next, true
}

type streamWriter struct {
	wb       *WriteBuffer
	streamID StreamID
}

var _ io.Writer = (*streamWriter)(nil)

func (sw *streamWriter) Write(p []byte) (int, error) {
	return sw.wb.Write(p, sw.streamID)
}
