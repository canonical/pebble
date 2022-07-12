package servicelog

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
	host, err := os.Hostname()
	if err != nil {
		host = "localhost" // TODO: what is the best default here?
	}

	return &SyslogWriter{
		version:  1,
		dst:      dst,
		App:      app,
		Host:     host,
		pid:      os.Getpid(),
		msgid:    "-",     // This is the "nil" value per RFC 5424
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
	mu            sync.Mutex
	conn          net.Conn
	destHost      string
	serverCert    []byte
	protocol      string
	buf           *RingBuffer
	done          chan struct{}
	closed        bool
	waitReconnect time.Duration
}

func NewSyslogTransport(protocol string, destHost string, serverCert []byte) *SyslogTransport {
	transport := &SyslogTransport{
		serverCert: serverCert,
		destHost:   destHost,
		protocol:   protocol,
		done:       make(chan struct{}),
		buf:        NewRingBuffer(maxLogBytes),
	}
	go transport.forward()
	return transport
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

func (s *SyslogTransport) Write(p []byte) (int, error) {
	// octet framing as per RFC 5425
	_, err := fmt.Fprintf(s.buf, "%d %s", len(p), p)
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

func (s *SyslogTransport) forward() {
	iter := s.buf.HeadIterator(0)
	defer iter.Close()
	for iter.Next(s.done) {
		s.mu.Lock()
		err := s.connect()
		if err != nil {
			logger.Noticef("syslog transport failed connection: %v", err)
			s.mu.Unlock()
			continue
		}
		logger.Noticef("syslog transport forwarding message") // DEBUG

		_, err = io.Copy(s.conn, iter)
		if err != nil {
			s.conn = nil
			// TODO: accuulate these errors to return on Close? - maybe not
			logger.Noticef("syslog transport failed forwarding: %v", err)
			// TODO: save the bytes that we try to write to dest for retries (with backoff) if
			// there are errors since e.g. syslog servers could be intermittent in availability.
			// need to add rewind behavior for iterator. io.Copy uses WriteTo if available -
			// which seems like it should handle this okay, but confirm this.
		}
		s.mu.Unlock()
	}
}

func (s *SyslogTransport) connect() error {
	if s.conn != nil {
		return nil
	} else if s.closed {
		return fmt.Errorf("write to closed SyslogTransport")
	}

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
