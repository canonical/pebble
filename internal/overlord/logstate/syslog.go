package logstate

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"

	"github.com/canonical/pebble/internal/logger"
	"github.com/canonical/pebble/internal/servicelog"
)

const maxLogBytes = 100 * 1024

type SyslogWriter struct {
	mu       sync.RWMutex
	version  int
	dst      io.Writer
	App      string
	Host     string
	pid      int
	msgid    string
	priority int
	params   map[string]string
}

func NewSyslogWriter(dst io.Writer, app string) *SyslogWriter {
	// "-" is the "nil" value per RFC 5424
	return &SyslogWriter{
		version:  1,
		dst:      dst,
		App:      app,
		Host:     "-", // NOTE: could become useful to switch to os.Hostname()
		pid:      os.Getpid(),
		msgid:    "-",
		priority: 1*8 + 6, // for facility=user-msg severity=informational. See RFC 5424 6.2.1 for available codes.
		params:   make(map[string]string),
	}
}

func (s *SyslogWriter) SetParam(key, val string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.params[key] = val
}

func (s *SyslogWriter) SetPid(pid int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pid = pid
}

func (s *SyslogWriter) Write(p []byte) (int, error) {
	_, err := s.dst.Write(s.buildMsg(p))
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

func (s *SyslogWriter) buildMsg(p []byte) []byte {

	s.mu.RLock()
	defer s.mu.RUnlock()

	// format defined by RFC 5424
	timestamp := time.Now().Format(time.RFC3339)
	privEnterpriseNum := 28978 // num for Canonical Ltd
	structuredData := fmt.Sprintf("[pebble@%d", privEnterpriseNum)

	for key, value := range s.params {
		structuredData += fmt.Sprintf(" %s=\"%s\"", key, value)
	}
	structuredData += "]"

	msg := fmt.Sprintf("<%d>%d %s %s %s %d %s %s %s",
		s.priority, s.version, timestamp, s.Host, s.App, s.pid, s.msgid, structuredData, p)
	return []byte(msg)
}

type SyslogTransport struct {
	closed        bool
	mu            sync.Mutex
	conn          net.Conn
	waitReconnect time.Duration
	destHost      string
	serverCert    []byte
	protocol      string
	buf           *servicelog.RingBuffer
	done          chan struct{}
}

func NewSyslogTransport(protocol string, destHost string, serverCert []byte) *SyslogTransport {
	transport := &SyslogTransport{
		serverCert: serverCert,
		destHost:   destHost,
		protocol:   protocol,
		done:       make(chan struct{}),
		buf:        servicelog.NewRingBuffer(maxLogBytes),
	}
	go transport.forward()
	return transport
}

func (s *SyslogTransport) Update(protocol string, destHost string, serverCert []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.protocol = protocol
	s.destHost = destHost
	s.serverCert = serverCert
	s.waitReconnect = 0

	s.conn.Close()
	s.conn = nil
}

func (s *SyslogTransport) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.closed = true
	if s.conn != nil {
		return s.conn.Close()
	}
	s.conn = nil

	close(s.done)

	return nil
}

// Write takes a properly formatted syslog message and writes/sends it to the underlying syslog server.
func (s *SyslogTransport) Write(msg []byte) (int, error) {
	// Octet framing as per RFC 5425.  This needs to occur here rather than later in order to
	// preserve framing of syslog messages atomically.  Otherwise failed or partial sends (across
	// the network) would otherwise cause framing of multiple or partial messages at once.
	_, err := fmt.Fprintf(s.buf, "%d %s", len(msg), msg)
	if err != nil {
		return 0, err
	}
	return len(msg), nil
}

func (s *SyslogTransport) forward() {
	iter := s.buf.HeadIterator(0)
	defer iter.Close()
	for iter.Next(s.done) {
		err := s.send(iter)
		for err != nil {
			// Loop here to avoid calling iter.Next which would shift the iterator index forward
			// causing failed write/send to result in log data bytes being dropped/skipped.
			logger.Noticef("syslog transport send failed: %v", err)
			err = s.send(iter)
		}
	}
}

func (s *SyslogTransport) send(iter servicelog.Iterator) error {
	err := s.ensureConnected()
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	_, err = io.Copy(s.conn, iter)
	if err != nil {
		// The connection might be bad. Close and reset it for later reconnection.
		s.conn.Close()
		s.conn = nil
	}
	return err
}

func (s *SyslogTransport) ensureConnected() error {
	if s.conn != nil {
		return nil
	} else if s.closed {
		return fmt.Errorf("write to closed SyslogTransport")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.waitReconnect > 0 {
		time.Sleep(s.waitReconnect)
	}

	var conn net.Conn
	var err error
	if s.serverCert != nil {
		// TODO: Is this really what we want here?
		pool := x509.NewCertPool()
		pool.AppendCertsFromPEM(s.serverCert)
		config := &tls.Config{RootCAs: pool}
		conn, err = tls.Dial(s.protocol, s.destHost, config)
	} else {
		conn, err = net.Dial(s.protocol, s.destHost)
	}

	if err != nil {
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
	s.waitReconnect = 0

	if s.conn != nil {
		s.conn.Close()
	}
	s.conn = conn
	return nil
}