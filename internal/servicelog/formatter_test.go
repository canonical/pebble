// Copyright (c) 2021 Canonical Ltd
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License version 3 as
// published by the Free Software Foundation.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>

package servicelog_test

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internal/servicelog"
)

const (
	timeFormatRegex = `\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z`
)

type Crashy struct {
	sync.RWMutex
	is bool
}

func (c *Crashy) IsCrashy() bool {
	c.RLock()
	defer c.RUnlock()
	return c.is
}

func (c *Crashy) Set(is bool) {
	c.Lock()
	c.is = is
	c.Unlock()
}

var crashy = Crashy{is: false}

func syslogTestServer(done chan<- string) (addr string, closer io.Closer, wg *sync.WaitGroup) {
	wg = new(sync.WaitGroup)

	cert, err := tls.LoadX509KeyPair("test/cert.pem", "test/privkey.pem")
	if err != nil {
		log.Fatalf("failed to load TLS keypair: %v", err)
	}
	config := tls.Config{Certificates: []tls.Certificate{cert}}
	l, e := tls.Listen("tcp", "127.0.0.1:6515", &config)
	if e != nil {
		log.Fatalf("startServer failed: %v", e)
	}
	addr = l.Addr().String()
	wg.Add(1)
	go func() {
		defer wg.Done()
		runStreamSyslog(l, done, wg)
	}()
	return addr, l, wg
}

func runStreamSyslog(l net.Listener, done chan<- string, wg *sync.WaitGroup) {
	for {
		var c net.Conn
		var err error
		if c, err = l.Accept(); err != nil {
			return
		}
		wg.Add(1)
		go func(c net.Conn) {
			defer wg.Done()
			c.SetReadDeadline(time.Now().Add(5 * time.Second))
			b := bufio.NewReader(c)
			for ct := 1; !crashy.IsCrashy() || ct&7 != 0; ct++ {
				s, err := b.ReadString('\n')
				if err != nil {
					break
				}
				done <- s
			}
			c.Close()
		}(c)
	}
}

func TestSyslogWriter(t *testing.T) {
	done := make(chan string)
	addr, closer, wg := syslogTestServer(done)
	defer closer.Close()
	defer wg.Done()

	var serverCert []byte
	clientCert, err := tls.LoadX509KeyPair("test/client.pem", "test/client-privkey.pem")
	if err != nil {
		t.Fatalf("failed to load TLS keypair: %v", err)
	}
	w, err := servicelog.NewSyslogWriter(addr, clientCert, serverCert)
	if err != nil {
		t.Fatal(err)
	}

	want := "hello"
	fmt.Fprint(w, want)
	got := <-done
	if got != want {
		t.Errorf("wrong result: want %v, got %v", want, got)
	}
}

type formatterSuite struct{}

var _ = Suite(&formatterSuite{})

func (s *formatterSuite) TestFormat(c *C) {
	b := &bytes.Buffer{}
	w := servicelog.NewFormatWriter(b, "test")

	fmt.Fprintln(w, "first")
	fmt.Fprintln(w, "second")
	fmt.Fprintln(w, "third")

	c.Assert(b.String(), Matches, fmt.Sprintf(`
%[1]s \[test\] first
%[1]s \[test\] second
%[1]s \[test\] third
`[1:], timeFormatRegex))
}

func (s *formatterSuite) TestFormatSingleWrite(c *C) {
	b := &bytes.Buffer{}
	w := servicelog.NewFormatWriter(b, "test")

	fmt.Fprintf(w, "first\nsecond\nthird\n")

	c.Assert(b.String(), Matches, fmt.Sprintf(`
%[1]s \[test\] first
%[1]s \[test\] second
%[1]s \[test\] third
`[1:], timeFormatRegex))
}
