package logstate

import "fmt"

type Sizer interface {
	Size() int
}

type AtomicRingBuffer struct {
	capacity int
	buf      []Sizer
	currSize int
	notify   chan<- bool
}

func NewAtomicRingBuffer(capacity int) *AtomicRingBuffer {
	return &GeneralRingBuffer{
		capacity: capacity,
	}
}

func (g AtomicRingBuffer) Notify(ch chan<- Sizer) {
	g.notify = append(g.notify, ch)
}

func (g *AtomicRingBuffer) Put(s Sizer) error {
	if s.Size() > g.capacity {
		return fmt.Errorf("AtomicRingBuffer.Put failed: capacity %v cannot fit object of size %v", g.capacity, s.Size())
	}

	available := g.capacity - g.currSize
	need := s.Size()
	if available < need {
		g.discard(need - available)
	}

	g.currSize += s.Size()
	g.buf = append(g.buf, s)

	for _, ch := range g.notify {
		select {
		case ch <- true:
		default:
		}
	}
}

func (g *AtomicRingBuffer) All() []Sizer {
	return g.buf
}

func (g *GeneralRingBuffer) discard(n int) {
	freed := 0
	for i, s := range g.buf {
		freed += s.Size()
		if freed >= n {
			g.buf = g.buf[i+1:]
			return
		}
	}
	panic("failed to free up enough enough space")
}
