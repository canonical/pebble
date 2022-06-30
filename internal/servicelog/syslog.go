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

type SyslogWriter struct {
	conn       net.Conn
	destHost   string
	serverCert []byte
	version    int
	App        string
	Host       string
	Pid        int
	Msgid      string
	Priority   int
	Params     map[string]string
	frame      func([]byte) []byte
	format     func(*SyslogWriter, []byte) []byte
}

func NewSyslogWriter(destHost string, serverCert []byte) (*SyslogWriter, error) {
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
		App:        os.Args[0],
		Pid:        os.Getpid(),
		Host:       host,
		Msgid:      "-", // This is the "nil" value per RFC 5424
		Priority:   16,  // NOTE: see RFC 5424 6.2.1 for available codes
		Params:     map[string]string{},
		frame:      frameFunc,
		format:     formatFunc,
	}
	return s, s.connect()
}

func (s *SyslogWriter) connect() error {
	// TODO: Is this really what we want here?
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(s.serverCert)
	config := tls.Config{RootCAs: pool}
	conn, err := tls.Dial("tcp", s.destHost, &config)
	if err != nil {
		return err
	}
	if s.conn != nil {
		s.conn.Close()
	}
	s.conn = conn
	return nil
}

func (s *SyslogWriter) Close() error {
	if s.conn != nil {
		return s.conn.Close()
	}
	return nil
}

func (s *SyslogWriter) Write(p []byte) (int, error) {
	msg := s.frame(s.format(s, p))
	logger.Noticef("Sending syslog message: %s", msg) // DEBUG
	_, err := s.conn.Write(msg)
	if err != nil {
		// try to reconnect
		s.connect()
		logger.Noticef("syslog send error: %v", err) // DEBUG
		return 0, err
	} else { // DEBUG
		logger.Noticef("syslog sent successfully") // DEBUG
	} // DEBUG
	return len(p), nil
}

type MultiWriter struct {
	mu     sync.Mutex
	dsts   []io.Writer
	buf    []byte
	ch     chan []byte
	errors []error
}

func NewMultiWriter(dst ...io.Writer) *MultiWriter {
	mw := &MultiWriter{dsts: dst, ch: make(chan []byte)}
	go mw.forwardWrites()
	return mw
}

func (mw *MultiWriter) forwardWrites() {
	// NOTE: do we really want to go async here at the dest writer looping level - this means that the
	// slowest destination/sink dictates how slow we write to *all* destinations.  Or do we want to
	// async+buffer at the destination level (i.e. a separate buffer per destination?
	for data := range mw.ch {
		for _, dst := range mw.dsts {
			_, err := dst.Write(data)
			if err != nil {
				mw.errors = append(mw.errors, err)
			}
		}
	}
}

func (mw *MultiWriter) Close() error {
	mw.Flush()
	close(mw.ch)
	return nil // TODO: return accumulated errors here?
}

// Flush causes all buffered data to be written to the underlying destination writer.  This should
// be called after all writes have been completed.
func (mw *MultiWriter) Flush() error {
	mw.mu.Lock()
	defer mw.mu.Unlock()

	if len(mw.buf) == 0 {
		return nil
	}

	mw.write()
	return nil
}

func (mw *MultiWriter) Write(p []byte) (int, error) {
	mw.mu.Lock()
	defer mw.mu.Unlock()

	mw.buf = append(mw.buf, p...)
	mw.write()
	return len(p), nil
}

func (mw *MultiWriter) write() {
	select {
	case mw.ch <- mw.buf:
		mw.buf = mw.buf[:0]
	default:
	}
}
