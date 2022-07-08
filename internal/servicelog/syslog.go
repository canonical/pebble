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
	return s.dst.Write(s.buildMsg(p))
}

func (s *SyslogWriter) buildMsg(p []byte) []byte {
	logger.Noticef("building syslog message from: %s", p) // DEBUG

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
	logger.Noticef("built syslog message: %s", msg) // DEBUG
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
		logger.Noticef("transport failed to connect: %v", err) // DEBUG
		return 0, err
	}

	// octet framing as per RFC 5425
	framed := []byte(fmt.Sprintf("%d %s", len(p), p))

	logger.Noticef("Sending syslog message: %s", framed) // DEBUG

	_, err = s.conn.Write(framed)
	if err != nil {
		// try to reconnect and resend
		s.conn = nil
		logger.Noticef("    message send failed") // DEBUG
		return 0, err
	}
	logger.Noticef("    syslog sent successfully") // DEBUG
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
	dsts []io.WriteCloser
}

func NewMultiWriter(dsts ...io.Writer) *MultiWriter {
	bufs := []io.WriteCloser{}
	for _, dst := range dsts {
		buf := NewRingBuffer(maxLogBytes)
		bufs = append(bufs, buf)

		go func(buf *RingBuffer, dst io.Writer) {
			done := make(chan struct{})
			bufIterator := buf.HeadIterator(0)
			defer bufIterator.Close()

			notifyWrite := make(chan bool)
			bufIterator.Notify(notifyWrite)
			for bufIterator.Next(done) {
				logger.Noticef("  - writing content to destination") // DEBUG
				_, err := io.Copy(dst, bufIterator)
				// Retry writes without moving buffer position until we succeed since some writers
				// be intermittently up and down. The buffer may start truncating before then -
				// that's okay.
				for err != nil {
					logger.Noticef("failed write for one writer destination: %v", err)
					select {
					case <-notifyWrite:
					}
					_, err = io.Copy(dst, bufIterator)
				}
				logger.Noticef("  - finished writing content to destination") // DEBUG
			}
		}(buf, dst)
	}
	return &MultiWriter{dsts: bufs}
}

func (mw *MultiWriter) Write(p []byte) (n int, err error) {
	for _, dst := range mw.dsts {
		logger.Noticef("mutiwriter forwarding content to dst: %s", p) // DEBUG
		_, err = dst.Write(p)
		if err != nil {
			logger.Noticef("multiwriter failed to buffer content: %v", err)
		}
	}
	return len(p), nil
}

func (mw *MultiWriter) Close() error {
	for _, dst := range mw.dsts {
		dst.Close()
	}
	return nil // TODO: return accumulated errors here?
}
