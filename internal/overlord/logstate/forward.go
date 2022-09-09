package logstate

import (
	"fmt"
	"sync"
	"time"

	"github.com/canonical/pebble/internal/logger"
)

var maxLogBytes int = 100 * 1024

const canonicalPrivEnterpriseNum = 28978

// LogBackend defines an interface to facilitate support for different log destination "types"
// (e.g. syslog, loki, etc.).
type LogBackend interface {
	Send(*LogMessage) error
	Close() error
}

// LogMessage represents service-originated log content with metadata relevant to log forwarding.
type LogMessage struct {
	Service   string
	Message   []byte
	Timestamp time.Time
}

// Size returns the size (i.e. in bytes) of the message - useful for e.g. limiting memory usage for
// buffered logs.
func (l *LogMessage) Size() int {
	return len(l.Message)
}

type DestsFunc func(service string) ([]*LogDestination, error)

// LogForwarder is a simple writer that is used to intercept (stdout+stderr) output from running services
// and annotate it with relevant metadata (e.g. service name) before conveying it to the
// appropriate log destinations for the service.
// log destination.
type LogForwarder struct {
	service   string
	destsFunc DestsFunc
}

func NewLogForwarder(destsFunc DestsFunc, service string) *LogForwarder {
	return &LogForwarder{service: service, destsFunc: destsFunc}
}

func (l *LogForwarder) Write(p []byte) (int, error) {
	data := append([]byte{}, p...)
	// TODO: should we sync this timestamp with the one that goes to pebble's service log buffer?
	// If so, how?
	msg := &LogMessage{Message: data, Service: l.service, Timestamp: time.Now()}
	dests, err := l.destsFunc(l.service)
	if err != nil {
		return 0, fmt.Errorf("failed to forward log message: %v", err)
	}

	for _, dest := range dests {
		if err := dest.Send(msg); err != nil {
			logger.Noticef("failed to transmit service %q logs to destination %q: %v", l.service, dest.name, err)
			continue
		}
	}
	return len(p), nil
}

// LogDestination manages fowarding log content to some destination.  It can be configured to send
// logs to any sort of underlying destination (e.g. syslog, loki, etc.) by priming it with an
// appropriate backend.  Messages accumulate in a fixed-size internal buffer and are concurrently
// pushed/sent to the underlying backend.  If the backend is unable to keep up and the buffer fills
// up, logs are discarded incrementally as necessary in FIFO order.  LogDestination is safe to use
// concurrently.  Close should be called when the destination will no longer be used to clean up
// internal goroutines, etc.
type LogDestination struct {
	mu      sync.Mutex
	name    string
	buf     *LogBuffer
	notify  chan bool
	backend LogBackend
	done    chan struct{}
	closed  bool
}

func NewLogDestination(name string, backend LogBackend) *LogDestination {
	buf := NewLogBuffer(maxLogBytes)
	c := &LogDestination{
		name:    name,
		backend: backend,
		buf:     buf,
		notify:  make(chan bool, 1),
		done:    make(chan struct{}),
	}
	buf.Notify(c.notify)

	go c.run()
	return c
}

// SetBackend updates the backend to which log messages are sent.  This method is concurrency-safe.
func (c *LogDestination) SetBackend(b LogBackend) error {
	if c.closed {
		return fmt.Errorf("cannot modify a closed destination")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if b != c.backend {
		c.backend.Close()
	}
	c.backend = b
	return nil
}

// Send conveys msg to the underlying backend.
func (c *LogDestination) Send(msg *LogMessage) error {
	if c.closed {
		return fmt.Errorf("cannot send messages to a closed destination")
	}
	return c.buf.Put(msg)
}

// Close marks the destination as no longer available for use.  Further calls to Send will return
// an error.  Internal resources/goroutines will be cleaned up.  Any unsent buffered logs will
// *not* be forwarded to the backend.
func (c *LogDestination) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	c.backend.Close()
	close(c.done)
}

func (c *LogDestination) run() {
	for {
		select {
		case <-c.notify:
			for _, msg := range c.buf.GetAll() {
				c.mu.Lock()
				err := c.backend.Send(msg)
				if err != nil {
					logger.Noticef("destination error: %v", err)
				}
				c.mu.Unlock()
			}
		case <-c.done:
			// TODO: should we try to drain the buffer first?
			return
		}
	}
}

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
		return fmt.Errorf("LogBuffer capacity %v cannot fit object of size %v", b.capacity, m.Size())
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
