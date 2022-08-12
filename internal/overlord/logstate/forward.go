package logstate

import (
	"sync"
	"time"
)

type LogForwarder struct {
	service string
}

func NewLogForwarder(t LogTransport, service string) *LogForwarder {
	return &LogForwarder{service: service, transport: t}
}

func (l *LogForwarder) Write(p []byte) (int, error) {
	data = append([]byte{}, p...)
	err := l.transport.Send(&LogMessage{Message: data, Service: l.service, Timestamp: time.Now()})
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

type LogMessage struct {
	Service   string
	Message   []byte
	Timestamp time.Time
}

func (l *LogMessage) Size() int {
	return len(Message)
}

type LogTransport struct {
	buf     *AtomicRingBuffer
	notify  chan bool
	backend LogBackend
}

type LogBackend interface {
	Send(*LogMessage) error
}

func NewLogTransport(backend LogBackend) {
	buf := NewAtomicRingBuffer(10000)
	notify := make(chan bool)
	buf.Notify(notify)
	t := &LokiTransport{
		backend: backend,
		buf:     buf,
		notify:  notify,
		done:    make(chan bool),
	}

	go t.run()
	return t
}

func (t *LokiTransport) run() {
	for {
		select {
		case <-t.notify:
			for _, m := range t.buf.All() {
				msg = m.(*LogMessage)
				t.backend.Send(msg)
			}
		case <-done:
			return
		}
	}
}

type LokiBackend struct {
	mu     sync.Mutex
	prefix string
	suffix string
}

func NewLokiBackend(addr string) *LokiBackend {
	return &LokiBackend{}
}

func (b *LokiBackend) Send(m *LogMessage) {
}

func (b *LokiBackend) UpdateLabels(labels map[string]string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.prefix = "..."
	b.suffix = "..."
}

var lokiMessageTmpl = `
{
  "streams": [
    {
      "stream": {
		{{.}}
      },
      "values": [
	    {{"{{.}}"}}
      ]
    }
  ]
}
`
