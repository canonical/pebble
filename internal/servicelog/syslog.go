package servicelog

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/canonical/pebble/internal/logger"
)

type SyslogWriter struct {
	conn   net.Conn
	frame  func([]byte) []byte
	format func([]byte) []byte
}

func NewSyslogWriter(host string, serverCert []byte) (*SyslogWriter, error) {
	// TODO: Is this really what we want here?
	var pool *x509.CertPool
	if serverCert != nil {
		pool = x509.NewCertPool()
		pool.AppendCertsFromPEM(serverCert)
	}
	config := tls.Config{RootCAs: pool}

	conn, err := tls.Dial("tcp", host, &config)
	if err != nil {
		return nil, err
	}

	frame := func(p []byte) []byte { return []byte(fmt.Sprintf("%d %s", len(p), p)) }
	format := func(content []byte) []byte {
		timestamp := time.Now().Format(time.RFC3339)
		pid := 42         // TODO: Make this be service PID
		app := "foo"      // TODO: make this be service name
		hostname := "bar" // TODO: make this be workload container host
		tag := "FOO_TAG"
		priority := 14 // NOTE: see RFC 5424 6.2.1 for available codes
		version := 1
		msg := fmt.Sprintf("<%d>%d %s %s %s %d %s - %s",
			priority, version, timestamp, hostname, app, pid, tag, content)
		return []byte(msg)
	}

	return &SyslogWriter{conn: conn, frame: frame, format: format}, nil
}

func (s *SyslogWriter) Close() error { return s.conn.Close() }

func (s *SyslogWriter) Write(p []byte) (int, error) {
	logger.Noticef("Sending syslog message %s", p)
	msg := s.frame(s.format(p))

	go func() {
		_, err := s.conn.Write(msg)
		if err != nil {
			logger.Noticef("syslog send error: %v", err)
		} else {
			logger.Noticef("syslog sent successfully")
		}
	}()
	return len(p), nil
}

type BranchWriter struct {
	mu     sync.Mutex
	dsts   []io.Writer
	buf    []byte
	ch     chan []byte
	errors []error
}

func NewBranchWriter(dst ...io.Writer) *BranchWriter {
	b := &BranchWriter{dsts: dst, ch: make(chan []byte)}
	go b.forwardWrites()
	return b
}

func (b *BranchWriter) forwardWrites() {
	// NOTE: do we really want to go async here at the branchling level - this means that the
	// slowest destination/sink dictates how slow we write to *all* destinations.  Or do we want to
	// async/buffer at the destination level?
	for data := range b.ch {
		for _, dst := range b.dsts {
			_, err := dst.Write(data)
			if err != nil {
				b.errors = append(b.errors, err)
			}
		}
	}
}

func (b *BranchWriter) Close() error {
	b.Flush()
	close(b.ch)
	return nil // TODO: return accumulated errors here?
}

// Flush causes all buffered data to be written to the underlying destination writer.  This should
// be called after all writes have been completed.
func (b *BranchWriter) Flush() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if len(b.buf) == 0 {
		return nil
	}

	b.write()
	return nil
}

func (b *BranchWriter) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.buf = append(b.buf, p...)
	b.write()
	return len(p), nil
}

func (b *BranchWriter) write() {
	select {
	case b.ch <- b.buf:
		b.buf = b.buf[:0]
	default:
	}
}
