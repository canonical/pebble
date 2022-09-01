package logstate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/canonical/pebble/internal/logger"
)

var maxLogBytes int = 100 * 1024

const canonicalPrivEnterpriseNum = 28978

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
	service     string
	destination *LogDestination
}

func NewLogForwarder(c *LogDestination, service string) *LogForwarder {
	return &LogForwarder{service: service, destination: c}
}

func (l *LogForwarder) Write(p []byte) (int, error) {
	data := append([]byte{}, p...)
	// TODO: should we sync this timestamp with the one that goes to pebble's service log buffer?
	// If so, how?
	err := l.destination.Send(&LogMessage{Message: data, Service: l.service, Timestamp: time.Now()})
	if err != nil {
		logger.Noticef("log destination write failed: %v", err)
		return 0, err
	}
	return len(p), nil
}

type LogDestination struct {
	mu      sync.Mutex
	buf     *LogBuffer
	notify  chan bool
	backend LogBackend
	done    chan struct{}
	closed  bool
}

func NewLogDestination(backend LogBackend) *LogDestination {
	buf := NewLogBuffer(maxLogBytes)
	c := &LogDestination{
		backend: backend,
		buf:     buf,
		notify:  make(chan bool, 1),
		done:    make(chan struct{}),
	}
	buf.Notify(c.notify)

	go c.run()
	return c
}

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

func (c *LogDestination) UpdateLabels(labels map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.backend.UpdateLabels(labels)
}

func (c *LogDestination) Send(msg *LogMessage) error {
	if c.closed {
		return fmt.Errorf("cannot send messages to a closed destination")
	}
	return c.buf.Put(msg)
}

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
			return
		}
	}
}

type LokiBackend struct {
	address string
	labels  map[string]string
}

func NewLokiBackend(address string, labels map[string]string) (*LokiBackend, error) {
	u, err := url.Parse(address)
	if err != nil || u.Host == "" {
		u, err = url.Parse("//" + address)
	}
	if err != nil {
		return nil, fmt.Errorf("invalid loki server address: %v", err)
	} else if u.Scheme != "" && u.Scheme != "http" {
		return nil, fmt.Errorf("unsupported loki address scheme '%v'", u.Scheme)
	} else if u.RequestURI() != "" {
		return nil, fmt.Errorf("invalid loki address: extraneous path %q", u.RequestURI())
	}

	// check for and set loki defaults if ommitted from address
	if u.Scheme == "" {
		u.Scheme = "http"
	}
	if u.Port() == "" {
		u.Host += ":3100"
	}

	b := &LokiBackend{address: address}
	b.UpdateLabels(labels)
	return b, nil
}

func (b *LokiBackend) Close() error { return nil }

func (b *LokiBackend) UpdateLabels(labels map[string]string) {
	tmp := make(map[string]string)
	for k, v := range labels {
		tmp[k] = v
	}
	b.labels = tmp
}

type lokiMessageStream struct {
	Stream map[string]string `json:"stream"`
	Values [][2]string       `json:"values"`
}

type lokiMessage struct {
	Streams []lokiMessageStream `json:"streams"`
}

func (b *LokiBackend) Send(m *LogMessage) error {
	b.labels["pebble_service"] = m.Service
	timestamp := strconv.FormatInt(m.Timestamp.UnixNano(), 10)
	data, err := json.Marshal(lokiMessage{
		Streams: []lokiMessageStream{
			lokiMessageStream{
				Stream: b.labels,
				Values: [][2]string{{timestamp, string(m.Message)}},
			},
		}})
	if err != nil {
		logger.Noticef("failed to build loki message: %v", err)
	}

	buf := bytes.NewBuffer(data)
	addr := "http://" + b.address + "/loki/api/v1/push"
	logger.Noticef("loki backend is sending message (addr=%v):\n%v", addr, buf.String())
	r, err := http.NewRequest("POST", addr, buf)
	if err != nil {
		logger.Noticef("loki send failed: %v", err)
		return err
	}
	r.Header.Add("Content-Type", "application/json")
	c := &http.Client{}
	resp, err := c.Do(r)
	if err != nil {
		logger.Noticef("loki send failed: %v", err)
	}
	defer resp.Body.Close()
	data, _ = ioutil.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		logger.Noticef("%s", data)
	}
	return err
}

// SyslogBackend takes writes and formats them according to RFC5424.  The formatted syslog messages
// are then forwarded to the specified underlying destination io.Writer.  SyslogWriter is safe for
// concurrent writes and use.
type SyslogBackend struct {
	mu             sync.RWMutex
	version        int
	host           string
	msgid          string
	priority       int
	pid            string
	structuredData string

	conn          net.Conn
	address       *url.URL
	waitReconnect time.Duration
	closed        bool
}

// NewSyslogBackend creates a writer forwarding writes as syslog messages to dst.  The forwarded
// messages will have app as the application name.  Other message parameters are set using
// reasonable defaults or the RFC5424 nil value "-".  labels contains key-value pairs to be
// attached to syslog messages in their structured data section (see. RFC5424 section 6.3).
// *Every* write/message forwarded will include these parameters.
func NewSyslogBackend(addr string, labels map[string]string) (*SyslogBackend, error) {
	u, err := url.Parse(addr)
	if err != nil || u.Host == "" {
		u, err = url.Parse("//" + addr)
	}
	if err != nil {
		return nil, err
	} else if u.Scheme != "tcp" && u.Scheme != "udp" && u.Scheme != "" {
		return nil, fmt.Errorf("invalid syslog server address scheme %q", u.Scheme)
	}
	if u.Scheme == "" {
		u.Scheme = "tcp"
	}

	return &SyslogBackend{
		version:        1,
		pid:            "-",
		host:           "-",
		msgid:          "-",
		priority:       1*8 + 6, // for facility=user-msg severity=informational. See RFC 5424 6.2.1 for available codes.
		structuredData: syslogStructuredData("pebble", canonicalPrivEnterpriseNum, labels),

		address: u,
	}, nil
}

// syslogStructuredData formats the given labels into a structured data section for a syslog message
// according to RFC5424 section 6.
func syslogStructuredData(name string, enterpriseNum int, labels map[string]string) string {
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
	s.structuredData = syslogStructuredData("pebble", canonicalPrivEnterpriseNum, labels)
}

func (s *SyslogBackend) Send(m *LogMessage) error {
	err := s.ensureConnected()
	if err != nil {
		return err
	}

	s.mu.RLock()
	_, err = s.conn.Write(s.buildMsg(m))
	s.mu.RUnlock()

	if err != nil {
		// The connection might be bad. Close and reset it for later reconnection attempt(s).
		s.mu.Lock()
		s.conn.Close()
		s.mu.Unlock()
	}
	return err
}

func (s *SyslogBackend) buildMsg(m *LogMessage) []byte {
	// format defined by RFC 5424
	timestamp := m.Timestamp.Format(time.RFC3339)
	msg := fmt.Sprintf("<%d>%d %s %s %s %s %s %s %s",
		s.priority, s.version, timestamp, s.host, m.Service, s.pid, s.msgid, s.structuredData, m.Message)

	// Octet framing as per RFC 5425.
	framed := fmt.Sprintf("%d %s", len(msg), msg)
	return []byte(framed)
}

func (s *SyslogBackend) Close() error {
	s.closed = true
	func() {
		s.mu.RLock()
		defer s.mu.RUnlock()
		if s.conn != nil {
			s.conn.SetDeadline(time.Now().Add(-1 * time.Second))
		}
	}()

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn == nil {
		return nil
	}
	err := s.conn.Close()
	s.conn = nil
	return err
}

func (s *SyslogBackend) ensureConnected() error {
	if s.conn != nil {
		return nil
	} else if s.closed {
		return fmt.Errorf("write to closed SyslogBackend")
	}

	if s.waitReconnect > 0 {
		time.Sleep(s.waitReconnect)
	}

	conn, err := net.Dial(s.address.Scheme, s.address.Host)

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
