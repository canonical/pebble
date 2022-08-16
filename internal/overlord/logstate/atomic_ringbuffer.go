package logstate

import (
	"fmt"
	"sync"
)

type AtomicRingBuffer struct {
	mu       sync.Mutex
	capacity int
	buf      []*LogMessage
	currSize int
	notify   []chan<- bool
}

func NewAtomicRingBuffer(capacity int) *AtomicRingBuffer {
	return &AtomicRingBuffer{
		capacity: capacity,
	}
}

func (g *AtomicRingBuffer) Notify(ch chan<- bool) {
	g.notify = append(g.notify, ch)
}

func (g *AtomicRingBuffer) Put(m *LogMessage) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if m.Size() > g.capacity {
		return fmt.Errorf("AtomicRingBuffer.Put failed: capacity %v cannot fit object of size %v", g.capacity, m.Size())
	}

	available := g.capacity - g.currSize
	need := m.Size()
	if available < need {
		g.discard(need - available)
	}

	g.currSize += m.Size()
	g.buf = append(g.buf, m)

	for _, ch := range g.notify {
		select {
		case ch <- true:
		default:
		}
	}
	return nil
}

func (g *AtomicRingBuffer) GetAll() []*LogMessage {
	g.mu.Lock()
	defer g.mu.Unlock()
	buf := g.buf
	g.buf = g.buf[:0]
	return buf
}

func (g *AtomicRingBuffer) discard(n int) {
	freed := 0
	for i, m := range g.buf {
		freed += m.Size()
		if freed >= n {
			g.buf = g.buf[i+1:]
			return
		}
	}
	panic("failed to free up enough enough space")
}
