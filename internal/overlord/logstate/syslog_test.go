// Copyright (c) 2023 Canonical Ltd
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

package logstate

import (
	"bufio"
	"net"
	"net/url"
	"time"

	"github.com/canonical/pebble/internal/servicelog"
	. "gopkg.in/check.v1"
)

type syslogSuite struct{}

var _ = Suite(&syslogSuite{})

func (s *syslogSuite) TestEncodeEntry(c *C) {
	client := &syslogClient{
		version:        1,
		pid:            "-",
		host:           "mycontainer",
		msgid:          "-",
		priority:       priorityVal(FacilityUserLevelMessage, SeverityInformational),
		structuredData: syslogStructuredData("pebble", canonicalPrivEnterpriseNum, nil),
	}
	client.encodeEntry(servicelog.Entry{
		Time:    time.Date(2021, 5, 26, 12, 37, 1, 0, time.UTC),
		Service: "foo",
		Message: "this is a log entry",
	})
	c.Check(client.data.String(), Equals, "68 <14>1 2021-05-26T12:37:01Z mycontainer foo - - - this is a log entry")
}

func (s *syslogSuite) TestEncodeEntryWithStructuredData(c *C) {
	client := &syslogClient{
		version:  1,
		pid:      "-",
		host:     "mycontainer",
		msgid:    "-",
		priority: priorityVal(FacilityUserLevelMessage, SeverityInformational),
		structuredData: syslogStructuredData("pebble", canonicalPrivEnterpriseNum,
			map[string]string{
				"foo": "bar",
				"baz": "bing",
			},
		),
	}
	client.encodeEntry(servicelog.Entry{
		Time:    time.Date(2021, 5, 26, 12, 37, 1, 0, time.UTC),
		Service: "foo",
		Message: "this is a log entry",
	})
	c.Check(client.data.String(), Equals, `102 <14>1 2021-05-26T12:37:01Z mycontainer foo - - [pebble@28978 foo="bar" baz="bing"] this is a log entry`)
}

func (s *syslogSuite) TestSyslogClient(c *C) {
	// Start a fake syslog server
	ln, err := net.Listen("tcp", "localhost:0") // will select unused port
	c.Assert(err, IsNil)
	defer ln.Close()

	messages := make(chan string)
	srv := &testSyslogServer{
		listener: ln,
		messages: messages,
	}

	serverStopped := make(chan struct{})
	go func() {
		err := srv.run()
		c.Assert(err, IsNil)
		close(serverStopped)
	}()

	// Create a syslog client
	serverAddr := ln.Addr().String()
	u, err := url.Parse("tcp://" + serverAddr)
	c.Assert(err, IsNil)
	client := &syslogClient{
		version:        1,
		pid:            "-",
		host:           "mycontainer",
		msgid:          "-",
		priority:       priorityVal(FacilityUserLevelMessage, SeverityInformational),
		structuredData: syslogStructuredData("pebble", canonicalPrivEnterpriseNum, nil),
		address:        u,
	}

	// Send a message to the syslog server
	entries := []servicelog.Entry{{
		Time:    time.Date(2023, 1, 31, 1, 23, 45, 0, time.UTC),
		Service: "foobar",
		Message: "test message #1",
	}, {
		Time:    time.Date(2023, 1, 31, 1, 23, 46, 0, time.UTC),
		Service: "foobar",
		Message: "test message #2",
	}, {
		Time:    time.Date(2023, 1, 31, 1, 23, 47, 0, time.UTC),
		Service: "foobar",
		Message: "test message #3",
	}}
	err = client.Send(entries)
	c.Assert(err, IsNil)
	err = client.Close()
	c.Assert(err, IsNil)

	// Check received message
	select {
	case msg := <-messages:
		c.Check(msg, DeepEquals,
			"67 <14>1 2023-01-31T01:23:45Z mycontainer foobar - - - test message #1"+
				"67 <14>1 2023-01-31T01:23:46Z mycontainer foobar - - - test message #2"+
				"67 <14>1 2023-01-31T01:23:47Z mycontainer foobar - - - test message #3",
		)
	case <-time.After(1 * time.Second):
		c.Fatal("timed out waiting for message")
	}

	// Check server stops correctly
	err = srv.close()
	c.Assert(err, IsNil)
	select {
	case <-serverStopped:
	case <-time.After(1 * time.Second):
		c.Fatal("timed out waiting for syslog server to stop")
	}
}

type testSyslogServer struct {
	listener net.Listener
	messages chan string
}

func (s *testSyslogServer) close() error {
	return s.listener.Close()
}

func (s *testSyslogServer) run() error {
	conn, err := s.listener.Accept()
	if err != nil {
		return err
	}
	defer conn.Close()

	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		s.messages <- scanner.Text()
	}
	return scanner.Err()
}
