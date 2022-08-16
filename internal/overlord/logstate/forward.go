package logstate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/canonical/pebble/internal/logger"
)

type LogBackend interface {
	Send(*LogMessage) error
	UpdateLabels(labels map[string]string)
	Close() error
}

type LogMessage struct {
	Service   string
	Message   []byte
	Timestamp time.Time
}

func (l *LogMessage) Size() int {
	return len(l.Message)
}

type LogForwarder struct {
	service   string
	collector *LogCollector
}

func NewLogForwarder(c *LogCollector, service string) *LogForwarder {
	return &LogForwarder{service: service, collector: c}
}

func (l *LogForwarder) Write(p []byte) (int, error) {
	data := append([]byte{}, p...)
	err := l.collector.Send(&LogMessage{Message: data, Service: l.service, Timestamp: time.Now()})
	if err != nil {
		logger.Noticef("log destination write failed: %v", err)
		return 0, err
	}
	return len(p), nil
}

type LogCollector struct {
	mu      sync.Mutex
	buf     *AtomicRingBuffer
	notify  chan bool
	backend LogBackend
	done    chan struct{}
	closed  bool
}

func NewLogCollector(backend LogBackend) *LogCollector {
	buf := NewAtomicRingBuffer(maxLogBytes)
	c := &LogCollector{
		backend: backend,
		buf:     buf,
		notify:  make(chan bool, 1),
		done:    make(chan struct{}),
	}
	buf.Notify(c.notify)

	go c.run()
	return c
}

func (c *LogCollector) SetBackend(b LogBackend) error {
	if c.closed {
		return fmt.Errorf("cannot modify a closed collector")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if b != c.backend {
		c.backend.Close()
	}
	c.backend = b
	return nil
}

func (c *LogCollector) UpdateLabels(labels map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.backend.UpdateLabels(labels)
}

func (c *LogCollector) Send(msg *LogMessage) error {
	if c.closed {
		return fmt.Errorf("cannot send messages to a closed collector")
	}
	return c.buf.Put(msg)
}

func (c *LogCollector) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	c.backend.Close()
	close(c.done)
}

func (c *LogCollector) run() {
	for {
		select {
		case <-c.notify:
			for _, msg := range c.buf.GetAll() {
				c.mu.Lock()
				err := c.backend.Send(msg)
				if err != nil {
					logger.Noticef("collector error: %v", err)
				}
				c.mu.Unlock()
			}
		case <-c.done:
			return
		}
	}
}

type LokiBackend struct {
	address string
	prefix  string
	suffix  string
}

const lokiPrefix = `
{"streams":
   [
	  {
	    "stream": {%s},
		 "values": [
			[`
const lokiSuffix = `]
		 ]
	  }
   ]
}`

func (b *LokiBackend) buildPrefix(labels map[string]string) {
	var labeltext []string
	for key, val := range labels {
		labeltext = append(labeltext, fmt.Sprintf("%q: %q", key, val))
	}
	b.prefix = fmt.Sprintf(lokiPrefix, strings.Join(labeltext, ","))
}

func NewLokiBackend(address string, labels map[string]string) *LokiBackend {
	b := &LokiBackend{address: address}
	b.buildPrefix(labels)
	return b
}

func (b *LokiBackend) Close() error { return nil }

func (b *LokiBackend) UpdateLabels(labels map[string]string) {
	b.buildPrefix(labels)
}

func (b *LokiBackend) Send(m *LogMessage) error {
	timestamp := m.Timestamp.Format(time.RFC3339)
	msg, err := json.Marshal(string(m.Message))
	if err != nil {
		return err
	}

	var buf bytes.Buffer
	buf.WriteString(b.prefix)
	fmt.Fprintf(&buf, "%q, %s", timestamp, msg)
	buf.WriteString(lokiSuffix)

	addr := "http://" + b.address + "/loki/api/v1/push"
	logger.Noticef("loki backend is sending message (addr=%v):\n%v", addr, buf.String())
	r, err := http.NewRequest("POST", addr, &buf)
	if err != nil {
		logger.Noticef("loki send failed: %v", err)
		return err
	}
	r.Header.Add("Content-Type", "application/json")
	c := &http.Client{}
	_, err = c.Do(r)
	if err != nil {
		logger.Noticef("loki send failed: %v", err)
	}
	return err
}

// SyslogBackend takes writes and formats them according to RFC5424.  The formatted syslog messages
// are then forwarded to the specified underlying destination io.Writer.  SyslogWriter is safe for
// concurrent writes and use.
type SyslogBackend struct {
	version        int
	host           string
	msgid          string
	priority       int
	pid            string
	structuredData string

	conn          net.Conn
	address       string
	waitReconnect time.Duration
}

// NewSyslogBackend creates a writer forwarding writes as syslog messages to dst.  The forwarded
// messages will have app as the application name.  Other message parameters are set using
// reasonable defaults or the RFC5424 nil value "-".  labels contains key-value pairs to be
// attached to syslog messages in their structured data section (see. RFC5424 section 6.3).
// *Every* write/message forwarded will include these parameters.
func NewSyslogBackend(addr string, labels map[string]string) *SyslogBackend {
	return &SyslogBackend{
		version:        1,
		pid:            "-",
		host:           "-",
		msgid:          "-",
		priority:       1*8 + 6, // for facility=user-msg severity=informational. See RFC 5424 6.2.1 for available codes.
		structuredData: buildSyslogStructuredData("pebble", canonicalPrivEnterpriseNum, labels),

		address: addr,
	}
}

// buildStructuredData formats the given labels into a structured data section for a syslog message
// according to RFC5424 section 6.
func buildSyslogStructuredData(name string, enterpriseNum int, labels map[string]string) string {
	if len(labels) == 0 {
		return "-"
	}
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "[%s@%d", name, enterpriseNum)
	for key, value := range labels {
		fmt.Fprintf(&buf, " %s=\"", key)
		// escape the value according to RFC5424 6.3.3
		for i := 0; i < len(value); i++ {
			// don't use "for _, c := range value" as we don't want runes
			c := value[i]
			if c == '"' || c == '\\' || c == ']' {
				buf.WriteByte('\\')
			}
			buf.WriteByte(c)
		}
		buf.WriteByte('"')
	}
	buf.WriteByte(']')
	return buf.String()
}

func (s *SyslogBackend) UpdateLabels(labels map[string]string) {
	s.structuredData = buildStructuredData("pebble", canonicalPrivEnterpriseNum, labels)
}

func (s *SyslogBackend) Send(m *LogMessage) error {
	err := s.ensureConnected()
	if err != nil {
		return err
	}

	_, err = s.conn.Write(s.buildMsg(m))
	if err != nil {
		// The connection might be bad. Close and reset it for later reconnection attempt(s).
		s.conn.Close()
		s.conn = nil
	}
	return err
}

func (s *SyslogBackend) buildMsg(m *LogMessage) []byte {
	// format defined by RFC 5424
	timestamp := m.Timestamp.Format(time.RFC3339)
	msg := fmt.Sprintf("<%d>%d %s %s %s %s %s %s %s",
		s.priority, s.version, timestamp, s.host, m.Service, s.pid, s.msgid, s.structuredData, m.Message)

	// Octet framing as per RFC 5425.  This needs to occur here rather than later in order to
	// preserve framing of syslog messages atomically.  Otherwise failed or partial sends (across
	// the network) would otherwise cause framing of multiple or partial messages at once.
	framed := fmt.Sprintf("%d %s", len(msg), msg)
	return []byte(framed)
}

func (s *SyslogBackend) Close() error {
	return s.conn.Close()
}

func (s *SyslogBackend) ensureConnected() error {
	if s.conn != nil {
		return nil
	}

	if s.waitReconnect > 0 {
		time.Sleep(s.waitReconnect)
	}

	conn, err := net.Dial("tcp", s.address)

	if err != nil {
		// start an exponential backoff for reconnection attempts
		if s.waitReconnect == 0 {
			s.waitReconnect = 100 * time.Millisecond
		}
		newWait := 2 * s.waitReconnect
		if newWait > 10*time.Second {
			newWait = 10 * time.Second
		}
		s.waitReconnect = newWait
		return err
	}

	if s.conn != nil {
		s.conn.Close()
	}

	s.waitReconnect = 0 // reset backoff
	s.conn = conn
	return nil
}
