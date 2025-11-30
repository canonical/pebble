// Copyright (c) 2025 Canonical Ltd
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
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package syslog_test

import (
	"context"
	"net"
	"regexp"
	"time"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/overlord/logstate/syslog"
	"github.com/canonical/pebble/internals/servicelog"
)

func (*suite) TestUDPTransport(c *C) {
	// Create UDP listener
	conn, err := net.ListenPacket("udp", "localhost:0")
	c.Assert(err, IsNil)
	defer conn.Close()

	msgChan := make(chan string, 1)
	serverStopped := make(chan struct{})

	// Start UDP server
	go func() {
		buf := make([]byte, 8192)
		n, _, err := conn.ReadFrom(buf)
		if err == nil && n > 0 {
			msgChan <- string(buf[:n])
		}
		close(serverStopped)
	}()

	// Create UDP client
	client, err := syslog.NewClient(&syslog.ClientOptions{
		Location: "udp://" + conn.LocalAddr().String(),
		Hostname: "test-host",
	})
	c.Assert(err, IsNil)
	defer client.Close()

	// Add log entries
	err = client.Add(servicelog.Entry{
		Time:    time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC),
		Service: "test-svc",
		Message: "UDP test message",
	})
	c.Assert(err, IsNil)

	// Flush (sends UDP datagram)
	err = client.Flush(context.Background())
	c.Assert(err, IsNil)

	// Wait for message
	select {
	case msg := <-msgChan:
		// UDP messages should NOT have octet framing (no "<length> " prefix)
		c.Check(msg, Matches, `^<13>1 2024-01-01T10:00:00Z test-host test-svc - - - UDP test message$`)
	case <-time.After(2 * time.Second):
		c.Fatal("timed out waiting for UDP message")
	}

	// Wait for server to finish
	select {
	case <-serverStopped:
	case <-time.After(1 * time.Second):
		c.Fatal("timed out waiting for UDP server to stop")
	}
}

func (*suite) TestUDPWithLabels(c *C) {
	conn, err := net.ListenPacket("udp", "localhost:0")
	c.Assert(err, IsNil)
	defer conn.Close()

	msgChan := make(chan string, 1)
	go func() {
		buf := make([]byte, 8192)
		n, _, err := conn.ReadFrom(buf)
		if err == nil && n > 0 {
			msgChan <- string(buf[:n])
		}
	}()

	client, err := syslog.NewClient(&syslog.ClientOptions{
		Location: "udp://" + conn.LocalAddr().String(),
		Hostname: "test-host",
		SDID:     "test-app",
	})
	c.Assert(err, IsNil)
	defer client.Close()

	// Set labels
	client.SetLabels("app", map[string]string{
		"env":     "prod",
		"version": "1.0",
	})

	err = client.Add(servicelog.Entry{
		Time:    time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC),
		Service: "app",
		Message: "labeled message",
	})
	c.Assert(err, IsNil)

	err = client.Flush(context.Background())
	c.Assert(err, IsNil)

	select {
	case msg := <-msgChan:
		// Verify structured data is included (no octet framing)
		pattern := `^<13>1 2024-01-01T10:00:00Z test-host app - - \[test-app@28978 env="prod" version="1\.0"\] labeled message$`
		c.Check(msg, Matches, pattern)
	case <-time.After(2 * time.Second):
		c.Fatal("timed out waiting for UDP message")
	}
}

func (*suite) TestInvalidUDPLocation(c *C) {
	_, err := syslog.NewClient(&syslog.ClientOptions{
		Location: "http://localhost:514", // Invalid scheme
	})
	c.Check(err, ErrorMatches, `invalid syslog server location .*, must be in form "tcp://host:port" or "udp://host:port"`)
}

func (*suite) TestTCPStillWorks(c *C) {
	// Ensure TCP still works with octet framing
	listener, err := net.Listen("tcp", "localhost:0")
	c.Assert(err, IsNil)
	defer listener.Close()

	msgChan := make(chan string, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		buf := make([]byte, 8192)
		var fullMessage string
		for {
			n, err := conn.Read(buf)
			if n > 0 {
				fullMessage += string(buf[:n])
			}
			if err != nil {
				break
			}
		}
		if fullMessage != "" {
			msgChan <- fullMessage
		}
	}()

	client, err := syslog.NewClient(&syslog.ClientOptions{
		Location: "tcp://" + listener.Addr().String(),
		Hostname: "test-host",
	})
	c.Assert(err, IsNil)

	err = client.Add(servicelog.Entry{
		Time:    time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC),
		Service: "test",
		Message: "TCP test",
	})
	c.Assert(err, IsNil)

	err = client.Flush(context.Background())
	c.Assert(err, IsNil)

	err = client.Close()
	c.Assert(err, IsNil)

	select {
	case msg := <-msgChan:
		// TCP messages SHOULD have octet framing
		pattern := regexp.MustCompile(`^\d+ <13>1 2024-01-01T10:00:00Z test-host test - - - TCP test$`)
		c.Check(pattern.MatchString(msg), Equals, true, Commentf("Message: %q", msg))
	case <-time.After(2 * time.Second):
		c.Fatal("timed out waiting for TCP message")
	}
}
