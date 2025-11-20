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
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/overlord/logstate/syslog"
	"github.com/canonical/pebble/internals/servicelog"
)

type suite struct{}

var _ = Suite(&suite{})

func Test(t *testing.T) {
	TestingT(t)
}

func (*suite) TestAddEntries(c *C) {
	ln, err := net.Listen("tcp", "localhost:0")
	c.Assert(err, IsNil)
	defer ln.Close()

	msgChan := make(chan string, 1)
	srv := &testSyslogServer{
		listener: ln,
		msgChan:  msgChan,
	}

	serverStarted := make(chan struct{})
	go func() {
		close(serverStarted)
		_ = srv.run()
	}()
	defer srv.close()
	<-serverStarted

	client, err := syslog.NewClient(&syslog.ClientOptions{
		Location:   "tcp://" + ln.Addr().String(),
		TargetName: "test-target",
	})
	c.Assert(err, IsNil)
	defer client.Close()

	client.SetLabels("svc1", map[string]string{
		"env":     "test",
		"version": "0.0.1",
	})

	// label for svc3 is set and then removed BEFORE adding entries
	client.SetLabels("svc3", map[string]string{
		"to-be-removed": "to-be-removed",
	})
	client.SetLabels("svc3", nil)

	// label for svc4 is set and then removed AFTER adding entries
	client.SetLabels("svc4", map[string]string{
		"to-be-removed": "to-be-removed",
	})

	// label for svc5 is set but no entries are added for svc5
	client.SetLabels("svc5", map[string]string{
		"no-such-log": "no-such-log",
	})

	// Add entries from different services
	entries := []servicelog.Entry{{
		Time:    time.Date(2023, 12, 31, 12, 0, 0, 0, time.UTC),
		Service: "svc1",
		Message: "message from svc1",
	}, {
		Time:    time.Date(2023, 12, 31, 12, 0, 1, 0, time.UTC),
		Service: "svc2",
		Message: "msg from svc2",
	}, {
		Time:    time.Date(2023, 12, 31, 12, 0, 2, 0, time.UTC),
		Service: "svc1",
		Message: "long message from svc1",
	}, {
		Time:    time.Date(2023, 12, 31, 12, 0, 3, 0, time.UTC),
		Service: "svc3",
		Message: "log of svc3 doesn't have any labels",
	}, {
		Time:    time.Date(2023, 12, 31, 12, 0, 4, 0, time.UTC),
		Service: "svc4",
		Message: "multiline\nline2\nline3",
	}}

	for _, entry := range entries {
		err = client.Add(entry)
		c.Assert(err, IsNil)
	}

	// `Add` and `SetLabels` shouldn't be order-dependent
	client.SetLabels("svc2", map[string]string{
		"env":     "production",
		"version": "1.2.3",
		"owner":   "team-2",
	})
	client.SetLabels("svc4", map[string]string{})

	err = client.Flush(context.Background())
	c.Assert(err, IsNil)

	expectedMsg := `103 <14>1 2023-12-31T12:00:00Z JYsAcer svc1 - - [pebble@28978 env="test" version="0.0.1"] message from svc1` +
		`120 <14>1 2023-12-31T12:00:01Z JYsAcer svc2 - - [pebble@28978 env="production" owner="team-2" version="1.2.3"] msg from svc2` +
		`108 <14>1 2023-12-31T12:00:02Z JYsAcer svc1 - - [pebble@28978 env="test" version="0.0.1"] long message from svc1` +
		`81 <14>1 2023-12-31T12:00:03Z JYsAcer svc3 - - - log of svc3 doesn't have any labels` +
		`67 <14>1 2023-12-31T12:00:04Z JYsAcer svc4 - - - multiline` + "\nline2" + "\nline3"
	select {
	case msg := <-msgChan:
		c.Check(len(msg), Equals, len(expectedMsg))
		c.Check(msg, Equals, expectedMsg)
	case <-time.After(2 * time.Second):
		c.Fatal("timed out waiting for message")
	}
}

func (*suite) TestBufferFull(c *C) {
	client, err := syslog.NewClient(&syslog.ClientOptions{
		TargetName:        "tgt1",
		Location:          "tcp://fake:514",
		MaxRequestEntries: 3,
	})
	c.Assert(err, IsNil)

	addEntry := func(s string) {
		err := client.Add(servicelog.Entry{Message: s})
		c.Assert(err, IsNil)
	}

	// Check that the client's buffer is as expected
	buffer := syslog.GetBuffer(client)
	checkBuffer := func(expected []any) {
		if len(buffer) != len(expected) {
			c.Fatalf("buffer length is %v, expected %v", len(buffer), len(expected))
		}

		for i := range expected {
			// 'nil' means c.buffer[i] should be zero
			if expected[i] == nil {
				c.Assert(buffer[i], DeepEquals, syslog.EntryWithService{},
					Commentf("buffer[%d] should be zero, obtained %v", i, buffer[i]))
				continue
			}

			// Otherwise, check buffer message matches string
			msg := expected[i].(string)
			c.Assert(syslog.GetMessage(buffer[i]), Equals, msg)
		}
	}

	checkBuffer([]any{nil, nil, nil, nil, nil, nil})
	addEntry("1")
	checkBuffer([]any{"1", nil, nil, nil, nil, nil})
	addEntry("2")
	checkBuffer([]any{"1", "2", nil, nil, nil, nil})
	addEntry("3")
	checkBuffer([]any{"1", "2", "3", nil, nil, nil})
	addEntry("4")
	checkBuffer([]any{nil, "2", "3", "4", nil, nil})
	addEntry("5")
	checkBuffer([]any{nil, nil, "3", "4", "5", nil})
	addEntry("6")
	checkBuffer([]any{nil, nil, nil, "4", "5", "6"})
	addEntry("7")
	checkBuffer([]any{"5", "6", "7", nil, nil, nil})
}

func (*suite) TestFlushEmpty(c *C) {
	client, err := syslog.NewClient(&syslog.ClientOptions{
		Location:   "tcp://fake:514",
		TargetName: "test",
	})
	c.Assert(err, IsNil)

	// Flushing with no entries should be a no-op
	err = client.Flush(context.Background())
	c.Assert(err, IsNil)
}

func (*suite) TestInvalidLocation(c *C) {
	// Invalid scheme
	_, err := syslog.NewClient(&syslog.ClientOptions{
		Location: "http://example.com:514",
	})
	c.Assert(err, ErrorMatches, `invalid syslog server address scheme "http"`)

	// Valid schemes should work
	_, err = syslog.NewClient(&syslog.ClientOptions{
		Location: "tcp://localhost:514",
	})
	c.Assert(err, IsNil)

	_, err = syslog.NewClient(&syslog.ClientOptions{
		Location: "udp://localhost:514",
	})
	c.Assert(err, IsNil)
}

type testSyslogServer struct {
	listener net.Listener
	msgChan  chan string
}

func (s *testSyslogServer) close() error {
	return s.listener.Close()
}

func (s *testSyslogServer) run() error {
	conn, err := s.listener.Accept()
	if err != nil {
		return err
	}

	go func() {
		defer conn.Close()
		// Read with a timeout
		conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		buf := make([]byte, 4096)
		var fullMessage string
		for {
			n, err := conn.Read(buf)
			if n > 0 {
				fullMessage += string(buf[:n])
			}
			if err != nil {
				break
			}
			// Reset deadline for next read
			conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		}

		if fullMessage != "" {
			select {
			case s.msgChan <- fullMessage:
			case <-time.After(1 * time.Second):
				// Avoid blocking if no one is reading
			}
		}
	}()

	return nil
}
