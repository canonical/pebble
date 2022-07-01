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
	mu         sync.Mutex
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
	closed     bool
}

func NewSyslogWriter(destHost string, serverCert []byte) *SyslogWriter {
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
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.write(p, 0)
}

func (s *SyslogWriter) write(p []byte, nthTry int) (int, error) {
	err := s.connect()
	if err != nil {
		return 0, err
	}

	time.Sleep(30 * time.Second) // DEBUG

	msg := s.frame(s.format(s, p))
	logger.Noticef("Sending syslog message: %s", msg) // DEBUG
	_, err = s.conn.Write(msg)
	if err != nil {
		// try to reconnect and resend
		s.conn = nil
		const maxRetries = 3
		if nthTry < maxRetries {
			return s.write(p, nthTry+1)
		}
		logger.Noticef("    message send failed") // DEBUG
		return 0, err
	}
	logger.Noticef("    syslog sent successfully") // DEBUG
	return len(p), nil
}

func (s *SyslogWriter) connect() error {
	if s.conn != nil {
		return nil
	} else if s.closed {
		return fmt.Errorf("write to closed SyslogWriter")
	}

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

type MultiWriter struct {
	dsts   []io.Writer
	chans  []chan []byte
	errors [][]error
}

func NewMultiWriter(dsts ...io.Writer) *MultiWriter {
	chans := []chan []byte{}
	for _, d := range dsts {
		ch := make(chan []byte, 1)
		chans = append(chans, ch)
		go forwardWrites(ch, d)
	}
	return &MultiWriter{dsts: dsts, chans: chans}
}

// takes data from ch async style and buffers it in an internal buffer.  It async style forwards
// those writes to dst.  Data not sent due to failed writes is not dropped until the buffer gets
// too full.
func forwardWrites(ch <-chan []byte, dst io.Writer) {
	buf := []byte{}
	var mu sync.Mutex
	tryWrite := make(chan bool)

	go func() {
		for data := range ch {
			mu.Lock()
			buf = append(buf, data...)
			// TODO: check if buf is getting too big, drop entries as necessary
			mu.Unlock()

			select {
			case tryWrite <- true:
			default:
			}
		}
		close(tryWrite)
	}()

	for _ = range tryWrite {
		// take data from buffer
		mu.Lock()
		data := append([]byte{}, buf...)
		buf = buf[:0]
		mu.Unlock()

		// TODO: would we rather push these messages to dst discretized the same way they came into
		// the original channel/buffer?  Or keep it like it is where failures cause re-buffering of
		// data to be aggregated with future writes into a single write to the destination?
		_, err := dst.Write(data)
		if err != nil {
			// TODO: what do we do with these errors?
			logger.Debugf(" service log write forwarding failed: %v", err) // DEBUG

			// return data to buffer
			mu.Lock()
			buf = append(data, buf...)
			mu.Unlock()
			continue
		}
	}
}

func (mw *MultiWriter) Write(p []byte) (n int, err error) {
	for _, ch := range mw.chans {
		ch <- p
	}
	return len(p), nil
}

func (mw *MultiWriter) Close() error {
	for _, ch := range mw.chans {
		close(ch)
	}
	return nil // TODO: return accumulated errors here?
}
