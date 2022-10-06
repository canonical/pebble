package logstate

import (
	"fmt"
	"sync"
	"time"

	"github.com/canonical/pebble/internal/logger"
)

const maxLogBytes int = 100 * 1024

const canonicalPrivEnterpriseNum = 28978

// LogBackend defines an interface to facilitate support for different log target "types"
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

type TargetsFunc func(service string) ([]*LogTarget, error)

// LogForwarder is a simple writer that is used to intercept (stdout+stderr) output from running services
// and annotate it with relevant metadata (e.g. service name) before conveying it to the
// appropriate log target for the service.
// log target.
type LogForwarder struct {
	service     string
	targetsFunc TargetsFunc
}

func NewLogForwarder(targetsFunc TargetsFunc, service string) *LogForwarder {
	return &LogForwarder{service: service, targetsFunc: targetsFunc}
}

func (l *LogForwarder) Write(p []byte) (int, error) {
	data := append([]byte{}, p...)
	// TODO: should we sync this timestamp with the one that goes to pebble's service log buffer?
	// If so, how?
	msg := &LogMessage{Message: data, Service: l.service, Timestamp: time.Now()}
	targets, err := l.targetsFunc(l.service)
	if err != nil {
		return 0, fmt.Errorf("failed to forward log message: %w", err)
	}

	for _, target := range targets {
		if err := target.Send(msg); err != nil {
			logger.Noticef("failed to transmit service %q logs to target %q: %v", l.service, target.Name(), err)
			continue
		}
	}
	return len(p), nil
}

// LogTarget manages fowarding log content to some destination.  It can be configured to send
// logs to any kind of underlying target (e.g. syslog, loki, etc.) by priming it with an
// appropriate backend.  Messages accumulate in a fixed-size internal buffer and are concurrently
// pushed/sent to the underlying backend.  If the backend is unable to keep up and the buffer fills
// up, logs are discarded incrementally as necessary in FIFO order.  LogTarget is safe to use
// concurrently.  Close should be called when the target will no longer be used to clean up
// internal goroutines, etc.  LogTarget takes ownership of the backend it is initialized with
// and will handle calling Close on it.
type LogTarget struct {
	mu      sync.Mutex
	name    string
	buf     *LogBuffer
	notify  chan bool
	backend LogBackend
	done    chan struct{}
	closed  bool
}

func NewLogTarget(name string, backend LogBackend) *LogTarget {
	buf := NewLogBuffer(maxLogBytes)
	c := &LogTarget{
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

func (c *LogTarget) Name() string { return c.name }

// SetBackend updates the backend to which log messages are sent.  This method is concurrency-safe.
func (c *LogTarget) SetBackend(b LogBackend) error {
	if c.closed {
		return fmt.Errorf("cannot modify a closed log target")
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
func (c *LogTarget) Send(msg *LogMessage) error {
	if c.closed {
		return fmt.Errorf("cannot send messages to a closed log target")
	}
	return c.buf.Put(msg)
}

// Close marks the target as no longer available for use.  Further calls to Send will return
// an error.  Internal resources/goroutines will be cleaned up.  Any unsent buffered logs will
// *not* be forwarded to the backend.
func (c *LogTarget) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	c.backend.Close()
	close(c.done)
}

func (c *LogTarget) run() {
	for {
		select {
		case <-c.notify:
			for _, msg := range c.buf.GetAll() {
				c.mu.Lock()
				err := c.backend.Send(msg)
				if err != nil {
					logger.Noticef("log target error: %v", err)
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
	notify   []chan<- bool
}

func NewLogBuffer(capacity int) *LogBuffer {
	return &LogBuffer{
		capacity: capacity,
		buf:      make([]*LogMessage, 0, capacity),
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

	available := b.capacity - len(b.buf)
	need := m.Size()
	if available < need {
		err := b.discard(need - available)
		if err != nil {
			return err
		}
	}

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
	return buf
}

func (b *LogBuffer) discard(n int) error {
	freed := 0
	for i, m := range b.buf {
		freed += m.Size()
		if freed >= n {
			b.buf = b.buf[i+1:]
			return nil
		}
	}
	return fmt.Errorf("failed to free up enough enough space")
}
