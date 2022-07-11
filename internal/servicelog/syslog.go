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
	mu         sync.Mutex
	conn       net.Conn
	destHost   string
	serverCert []byte
	protocol   string
	closed     bool
}

func NewSyslogTransport(protocol string, destHost string, serverCert []byte) *SyslogTransport {
	return &SyslogTransport{
		serverCert: serverCert,
		destHost:   destHost,
		protocol:   protocol,
	}
}

// Don't emit an error at construction time if dialing/conn fails - remote server could come up later.
// Do try to reconnect on write failures - retry sending message so we don't lose it.
// Don't try to reconnect after "Close" has been called.

func (s *SyslogTransport) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.closed = true
	if s.conn != nil {
		return s.conn.Close()
	}
	s.conn = nil
	return nil
}

func (s *SyslogTransport) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	err := s.connect()
	if err != nil {
		return 0, err
	}

	// octet framing as per RFC 5425
	framed := []byte(fmt.Sprintf("%d %s", len(p), p))

	_, err = s.conn.Write(framed)
	if err != nil {
		// try to reconnect and resend
		s.conn = nil
		return 0, err
	}
	return len(p), nil
}

func (s *SyslogTransport) connect() error {
	if s.conn != nil {
		return nil
	} else if s.closed {
		return fmt.Errorf("write to closed SyslogTransport")
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
		return err
	}

	if s.conn != nil {
		s.conn.Close()
	}
	s.conn = conn
	return nil
}

type MultiWriter struct {
	bufs []*RingBuffer
	dsts []io.Writer
	done chan struct{}
}

func NewMultiWriter(dests ...io.Writer) *MultiWriter {
	bufs := make([]*RingBuffer, len(dests))
	dsts := make([]io.Writer, len(dests))
	done := make(chan struct{})
	for i := range dests {
		bufs[i] = NewRingBuffer(maxLogBytes)
		dsts[i] = dests[i]
		go func(j int) {
			iter := bufs[j].HeadIterator(0)
			defer iter.Close()
			for iter.Next(done) {
				_, err := io.Copy(dsts[j], iter)
				if err != nil {
					logger.Noticef("    MultiWriter failed pushing content to destination: %v", err)
					// TODO: save the bytes that we try to write to dest for retries (with backoff) if
					// there are errors since e.g. syslog servers could be intermittent in availability.
					// need to add rewind behavior for iterator. io.Copy uses WriteTo if available -
					// which seems like it should handle this okay, but iterator currently advances its
					// index regardless of whether the ring-buffer encounters errors.
				}
			}
		}(i)
	}
	return &MultiWriter{dsts: dsts, bufs: bufs, done: done}
}

func (mw *MultiWriter) Write(p []byte) (n int, err error) {
	for _, buf := range mw.bufs {
		_, err = buf.Write(p)
		if err != nil {
			logger.Noticef("MultiWriter: failed to buffer data: %v", err)
		}
	}
	return len(p), nil
}

func (mw *MultiWriter) Close() error {
	for _, buf := range mw.bufs {
		buf.Close()
	}
	close(mw.done)
	return nil // TODO: return accumulated errors here?
}
