package logstate

import (
	"bytes"
	"fmt"
	"net"
	"net/url"
	"sync"
	"time"

	"github.com/canonical/pebble/internal/plan"
	"io"
)

func init() {
	RegisterLogBackend("syslog", func(t *plan.LogTarget) (LogBackend, error) { return NewSyslogBackend(t.Location) })
}

func validateSyslog(t *plan.LogTarget) error {
	b, err := NewSyslogBackend(t.Location)
	if b != nil {
		b.Close()
	}
	return err
}

// SyslogBackend takes writes and formats them according to RFC5424.  The formatted syslog messages
// are then forwarded to the underlying syslog server.  SyslogWriter is safe for concurrent writes
// and use.
type SyslogBackend struct {
	version        int
	host           string
	msgid          string
	priority       int
	pid            string
	structuredData string // TODO: use this for future log labels

	// mu helps manage access to network connection-related internal state.  This should be
	// read-locked whenever using the syslog server connection and write-locked whenever
	// replacing/reconnecting/etc the syslog server connection.
	mu            sync.RWMutex
	conn          net.Conn
	address       *url.URL
	waitReconnect time.Duration
	closed        bool
}

// NewSyslogBackend creates a writer forwarding writes as syslog messages to dst.  The forwarded
// messages will have app as the application name.  Other message parameters are set using
// reasonable defaults or the RFC5424 nil value "-".
func NewSyslogBackend(addr string) (*SyslogBackend, error) {
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
		priority:       priorityVal(FacilityUserLevelMessage, SeverityInformational),
		structuredData: syslogStructuredData("pebble", canonicalPrivEnterpriseNum, nil),

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

func (s *SyslogBackend) Send(m *LogMessage) error {
	err := s.ensureConnected()
	if err != nil {
		return err
	}

	s.mu.RLock()
	err = s.buildMsg(m, s.conn)
	s.mu.RUnlock()

	if err != nil {
		// The connection might be bad. Close and reset it for later reconnection attempt(s).
		s.mu.Lock()
		s.conn.Close()
		s.mu.Unlock()
	}
	return err
}

func (s *SyslogBackend) buildMsg(m *LogMessage, w io.Writer) error {
	// format defined by RFC 5424
	timestamp := m.Timestamp.Format(time.RFC3339)
	msg := fmt.Sprintf("<%d>%d %s %s %s %s %s %s %s",
		s.priority, s.version, timestamp, s.host, m.Service, s.pid, s.msgid, s.structuredData, m.Message)

	// Octet framing as per RFC 5425.
	framed := fmt.Sprintf("%d %s", len(msg), msg)
	_, err := w.Write([]byte(framed))
	return err
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
		err = s.conn.Close()
		if err != nil {
			return err
		}
	}

	s.waitReconnect = 0 // reset backoff
	s.conn = conn
	return nil
}

// Syslog Priority values - see RFC 5424 6.2.1
const (
	// Facility values
	FacilityUserLevelMessage = 1

	// Severity values
	SeverityInformational = 6
)

// priorityVal calculates the syslog Priority value (PRIVAL) from the given
// Facility and Severity values. See RFC 5424, sec 6.2.1 for details.
func priorityVal(facility, severity int) int {
	return facility*8 + severity
}
