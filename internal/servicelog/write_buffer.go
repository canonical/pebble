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
	"math"
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
}

func (l *write) empty() bool {
	return l.time == time.Time{} && l.start == RingPos(0) && l.end == RingPos(0)
}

func (l *write) length() int {
	return int(l.end - l.start)
}

type WriteBuffer struct {
	ringBuffer *RingBuffer
	writes     []write

	writeMutex  sync.Mutex
	writeClosed bool

	iteratorMutex sync.RWMutex
	iteratorList  []*iterator

	tailIndex int32
	headIndex int32
	numWrites int32
}

var (
	ErrNoMoreLines  = errors.New("no more lines")
	ErrSlowIterator = errors.New("cannot discard write due to slow iterator")
)

func NewWriteBuffer(maxWrites, maxBytes int) *WriteBuffer {
	wb := WriteBuffer{
		writes:     make([]write, maxWrites),
		ringBuffer: NewRingBuffer(maxBytes),
		tailIndex:  0,
		headIndex:  -1,
		numWrites:  0,
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
	wb.releaseIterators()
	return nil
}

func (wb *WriteBuffer) StreamWriter(streamID StreamID) io.Writer {
	return &streamWriter{wb, streamID}
}

func (wb *WriteBuffer) Write(p []byte, streamID StreamID) (int, error) {
	written := 0
	defer func() {
		if written > 0 {
			wb.signalIterators()
		}
	}()

	writeTime := time.Now().UTC()

	wb.writeMutex.Lock()
	defer wb.writeMutex.Unlock()

	if wb.writeClosed {
		return 0, io.ErrClosedPipe
	}

	writeLength := len(p)
	if writeLength > wb.ringBuffer.Size() {
		writeLength = wb.ringBuffer.Size()
	}

	for writeLength > wb.ringBuffer.Available() || !wb.canAdvanceHead() {
		err := wb.releaseTail()
		if err == ErrSlowIterator {
			// TODO handle timeout
		} else if err != nil {
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
	wb.iteratorMutex.Lock()
	defer wb.iteratorMutex.Unlock()

	numWrites := atomic.LoadInt32(&wb.numWrites)
	iter := &iterator{
		wb:         wb,
		index:      int32(TailIndex),
		notifyChan: make(chan struct{}, 1),
		closeChan:  make(chan struct{}),
	}
	if numWrites > 0 {
		iter.index = atomic.LoadInt32(&wb.tailIndex)
		iter.skip = true
	}

	wb.iteratorList = append(wb.iteratorList, iter)
	return iter
}

// HeadIterator returns an iterator from the head of the stream.
// If last is a positive non-zero number, the iterator will start
// at most N writes back from the head.
// The caller must Close the iterator when finished.
func (wb *WriteBuffer) HeadIterator(last int) Iterator {
	wb.iteratorMutex.Lock()
	defer wb.iteratorMutex.Unlock()

	numWrites := atomic.LoadInt32(&wb.numWrites)
	iter := &iterator{
		wb:         wb,
		index:      int32(TailIndex),
		notifyChan: make(chan struct{}, 1),
		closeChan:  make(chan struct{}),
	}
	if numWrites > 0 {
		if last < 0 {
			last = 0
		}
		if last > int(numWrites) {
			last = int(numWrites)
		}
		headIndex := WriteIndex(atomic.LoadInt32(&wb.headIndex))
		if last > 0 {
			headIndex -= WriteIndex(last - 1)
			iter.skip = true
		}
		iter.index = int32(headIndex)
	}

	wb.iteratorList = append(wb.iteratorList, iter)
	return iter
}

func (wb *WriteBuffer) removeIterator(iter *iterator) {
	wb.iteratorMutex.Lock()
	defer wb.iteratorMutex.Unlock()

	for i, storedIter := range wb.iteratorList {
		if iter != storedIter {
			continue
		}
		close(iter.closeChan)
		wb.iteratorList[i] = wb.iteratorList[len(wb.iteratorList)-1]
		wb.iteratorList = wb.iteratorList[:len(wb.iteratorList)-1]
		return
	}
}

func (wb *WriteBuffer) getWrite(index WriteIndex) *write {
	idx := int(index) % len(wb.writes)
	return &wb.writes[idx]
}

func (wb *WriteBuffer) releaseTail() error {
	wb.iteratorMutex.Lock()
	defer wb.iteratorMutex.Unlock()
	numWrites := atomic.LoadInt32(&wb.numWrites)
	if numWrites == 0 {
		return ErrNoMoreLines
	}

	lowestReaderIndex := WriteIndex(math.MaxInt32)
	for _, iter := range wb.iteratorList {
		index := iter.readIndex()
		if index < lowestReaderIndex {
			lowestReaderIndex = index
		}
	}

	tailIndex := WriteIndex(atomic.AddInt32(&wb.tailIndex, 1) - 1)
	// lowestReaderIndex can be less than tailIndex because it could
	// be TailIndex (-1).
	if lowestReaderIndex <= tailIndex {
		// Restore tail index.
		atomic.AddInt32(&wb.tailIndex, -1)
		return ErrSlowIterator
	}
	l := wb.getWrite(tailIndex)
	if !l.empty() {
		err := wb.ringBuffer.Discard(l.start, l.end)
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

func (wb *WriteBuffer) signalIterators() {
	wb.iteratorMutex.RLock()
	defer wb.iteratorMutex.RUnlock()

	for _, iter := range wb.iteratorList {
		select {
		case iter.notifyChan <- struct{}{}:
		default:
		}
	}
}

func (wb *WriteBuffer) releaseIterators() {
	wb.iteratorMutex.Lock()
	defer wb.iteratorMutex.Unlock()

	for _, iter := range wb.iteratorList {
		close(iter.closeChan)
	}
	wb.iteratorList = nil
}

func (wb *WriteBuffer) advanceIterator(iter *iterator) bool {
	// only need the read lock here as the atomics in the iterator
	// deal with write consistency.
	wb.iteratorMutex.RLock()
	defer wb.iteratorMutex.RUnlock()
	current := iter.readIndex()
	if current == TailIndex {
		numWrites := atomic.LoadInt32(&wb.numWrites)
		if numWrites == 0 {
			return false
		}
		nextIndex := WriteIndex(atomic.LoadInt32(&wb.tailIndex))
		iter.storeIndex(nextIndex)
		return true
	}
	nextIndex := current + 1
	headIndex := WriteIndex(atomic.LoadInt32(&wb.headIndex))
	if nextIndex > headIndex {
		return false
	}
	iter.storeIndex(nextIndex)
	return true
}

type streamWriter struct {
	wb       *WriteBuffer
	streamID StreamID
}

var _ io.Writer = (*streamWriter)(nil)

func (sw *streamWriter) Write(p []byte) (int, error) {
	return sw.wb.Write(p, sw.streamID)
}
