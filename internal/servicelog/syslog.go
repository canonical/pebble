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
	mu         sync.RWMutex
	conn       net.Conn
	destHost   string
	serverCert []byte
	version    int
	App        string
	Host       string
	Protocol   string
	Pid        int
	Msgid      string
	Priority   int
	Params     map[string]string
	frame      func([]byte) []byte
	format     func(*SyslogWriter, []byte) []byte
	closed     bool
}

func NewSyslogWriter(protocol string, destHost string, appName string, serverCert []byte) *SyslogWriter {
	host, err := os.Hostname()
	if err != nil {
		host = "localhost"
	}

	// format defined by RFC 5424
	formatFunc := func(w *SyslogWriter, content []byte) []byte {
		timestamp := time.Now().Format(time.RFC3339)
		privEnterpriseNum := 28978 // num for Canonical Ltd
		structuredData := fmt.Sprintf("[pebble@%d", privEnterpriseNum)
		for key, value := range w.Params {
			structuredData += fmt.Sprintf(" %s=\"%s\"", key, value)
		}
		structuredData += "]"

		msg := fmt.Sprintf("<%d>%d %s %s %s %d %s %s %s",
			w.Priority, w.version, timestamp, w.Host, w.App, w.Pid, w.Msgid, structuredData, content)
		return []byte(msg)
	}

	// octet framing as per RFC 5425
	frameFunc := func(p []byte) []byte { return []byte(fmt.Sprintf("%d %s", len(p), p)) }

	s := &SyslogWriter{
		serverCert: serverCert,
		destHost:   destHost,
		version:    1,
		App:        appName,
		Pid:        os.Getpid(),
		Host:       host,
		Protocol:   protocol,
		Msgid:      "-",     // This is the "nil" value per RFC 5424
		Priority:   1*8 + 6, // for facility=user-msg severity=informational. See RFC 5424 6.2.1 for available codes.
		Params:     map[string]string{},
		frame:      frameFunc,
		format:     formatFunc,
	}
	return s
}

// Don't emit an error at construction time if dialing/conn fails - remote server could come up later.
// Do try to reconnect on write failures - retry sending message so we don't lose it.
// Don't try to reconnect after "Close" has been called.

func (s *SyslogWriter) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.closed = true
	if s.conn != nil {
		return s.conn.Close()
	}
	s.conn = nil
	return nil
}

func (s *SyslogWriter) Write(p []byte) (int, error) {
	err := s.connect()
	if err != nil {
		return 0, err
	}

	msg := s.frame(s.format(s, p))
	logger.Noticef("Sending syslog message: %s", msg) // DEBUG

	s.mu.RLock()
	defer s.mu.RUnlock()

	_, err = s.conn.Write(msg)
	if err != nil {
		// try to reconnect and resend
		s.conn = nil
		logger.Noticef("    message send failed") // DEBUG
		return 0, err
	}
	logger.Noticef("    syslog sent successfully") // DEBUG
	return len(p), nil
}

func (s *SyslogWriter) connect() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.conn != nil {
		return nil
	} else if s.closed {
		return fmt.Errorf("write to closed SyslogWriter")
	}

	// TODO: Is this really what we want here?
	var conn net.Conn
	var err error
	if s.serverCert != nil {
		pool := x509.NewCertPool()
		pool.AppendCertsFromPEM(s.serverCert)
		config := &tls.Config{RootCAs: pool}
		conn, err = tls.Dial(s.Protocol, s.destHost, config)
	} else {
		conn, err = net.Dial(s.Protocol, s.destHost)
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

				_, err := io.Copy(dst, bufIterator)
				// Retry writes without moving buffer position until we succeed since e.g. syslog
				// forwarding endpoints may be unreliable. The buffer may start truncating before
				// then - that's okay.
				for err != nil {
					logger.Noticef("log forward failed: %v", err)
					select {
					case <-notifyWrite:
					}
					_, err = io.Copy(dst, bufIterator)
				}
			}
		}(buf, dst)
	}
	return &MultiWriter{dsts: bufs}
}

func (mw *MultiWriter) Write(p []byte) (n int, err error) {
	for _, dst := range mw.dsts {
		_, err = dst.Write(p)
		if err != nil {
			logger.Noticef("multiwriter log forward buffering failed: %v", err)
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
