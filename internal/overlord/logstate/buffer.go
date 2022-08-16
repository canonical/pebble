package logstate

import (
	"fmt"
	"sync"
)

type LogBuffer struct {
	mu       sync.Mutex
	capacity int
	buf      []*LogMessage
	currSize int
	notify   []chan<- bool
}

func NewLogBuffer(capacity int) *LogBuffer {
	return &LogBuffer{
		capacity: capacity,
	}
}

func (b *LogBuffer) Notify(ch chan<- bool) {
	b.notify = append(b.notify, ch)
}

func (b *LogBuffer) Put(m *LogMessage) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if m.Size() > b.capacity {
		return fmt.Errorf("LogBuffer.Put failed: capacity %v cannot fit object of size %v", b.capacity, m.Size())
	}

	available := b.capacity - b.currSize
	need := m.Size()
	if available < need {
		b.discard(need - available)
	}

	b.currSize += m.Size()
	b.buf = append(b.buf, m)

	for _, ch := range b.notify {
		select {
		case ch <- true:
		default:
		}
	}
	return nil
}

func (b *LogBuffer) GetAll() []*LogMessage {
	b.mu.Lock()
	defer b.mu.Unlock()
	buf := b.buf
	b.buf = b.buf[:0]
	b.currSize = 0
	return buf
}

func (b *LogBuffer) discard(n int) {
	freed := 0
	for i, m := range b.buf {
		freed += m.Size()
		if freed >= n {
			b.buf = b.buf[i+1:]
			b.currSize -= freed
			return
		}
	}
	panic("failed to free up enough enough space")
}
