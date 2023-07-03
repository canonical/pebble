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
	"bytes"
	"io"
	"time"

	"github.com/canonical/pebble/internals/plan"
	"gopkg.in/yaml.v3"

	"github.com/canonical/pebble/internals/servicelog"

	. "gopkg.in/check.v1"
)

type gathererSuite struct{}

var _ = Suite(&gathererSuite{})

func (s *gathererSuite) TestGathererBufferFull(c *C) {
	recv := make(chan []servicelog.Entry)
	g := newLogGathererForTest(nil, 1*time.Hour, 5, recv)
	go g.loop()

	entries := []servicelog.Entry{{
		Time:    time.Date(2023, 1, 1, 14, 34, 56, 789, time.UTC),
		Service: "foobar",
		Message: "log line #1",
	}, {
		Time:    time.Date(2023, 1, 1, 14, 34, 57, 789, time.UTC),
		Service: "foobar",
		Message: "log line #2",
	}, {
		Time:    time.Date(2023, 1, 1, 14, 34, 58, 789, time.UTC),
		Service: "foobar",
		Message: "log line #3",
	}, {
		Time:    time.Date(2023, 1, 1, 14, 34, 59, 789, time.UTC),
		Service: "foobar",
		Message: "log line #4",
	}, {
		Time:    time.Date(2023, 1, 1, 14, 35, 14, 789, time.UTC),
		Service: "foobar",
		Message: "log line #5",
	}}

	for _, entry := range entries {
		c.Assert(g.buffer.IsFull(), Equals, false)
		g.addLog(entry)
	}
	c.Assert(g.buffer.IsFull(), Equals, true)

	select {
	case received := <-recv:
		c.Assert(received, DeepEquals, entries)
	case <-time.After(10 * time.Millisecond):
		c.Fatal("timeout waiting to receive logs")
	}
}

func (s *gathererSuite) TestGathererTimeout(c *C) {
	recv := make(chan []servicelog.Entry)
	g := newLogGathererForTest(nil, 1*time.Microsecond, 2, recv)
	go g.loop()

	entry := servicelog.Entry{
		Time:    time.Date(2023, 1, 1, 14, 34, 56, 789, time.UTC),
		Service: "foobar",
		Message: "this is a log",
	}
	g.addLog(entry)
	c.Assert(g.buffer.IsFull(), Equals, false)

	select {
	case entries := <-recv:
		c.Assert(entries, DeepEquals, []servicelog.Entry{entry})
	case <-time.After(10 * time.Millisecond):
		c.Fatal("timeout waiting to receive logs")
	}
}

func (s *gathererSuite) TestGathererStop(c *C) {
	recv := make(chan []servicelog.Entry)
	g := newLogGathererForTest(nil, 1*time.Hour, 5, recv)
	go g.loop()

	entry := servicelog.Entry{
		Time:    time.Date(2023, 1, 1, 14, 34, 56, 789, time.UTC),
		Service: "foobar",
		Message: "this is a log",
	}
	g.addLog(entry)
	c.Assert(g.buffer.IsFull(), Equals, false)

	g.stop()
	select {
	case entries := <-recv:
		c.Assert(entries, DeepEquals, []servicelog.Entry{entry})
	case <-time.After(10 * time.Millisecond):
		c.Fatal("timeout waiting to receive logs")
	}
}

func newLogGathererForTest(
	target *plan.LogTarget,
	tickPeriod time.Duration, bufferCapacity int, recv chan []servicelog.Entry,
) *logGatherer {
	g := newLogGatherer(target)
	g.tickPeriod = tickPeriod
	g.buffer = &testBuffer{
		capacity: bufferCapacity,
	}
	g.client = &testClient{recv: recv}
	return g
}

// testBuffer is a "fake" implementation of logBuffer, for use in testing.
// It stores log entries internally in a slice.
type testBuffer struct {
	entries  []servicelog.Entry
	capacity int
}

func (b *testBuffer) Write(entry servicelog.Entry) {
	b.entries = append(b.entries, entry)
}

func (b *testBuffer) IsEmpty() bool {
	return len(b.entries) == 0
}

func (b *testBuffer) IsFull() bool {
	return len(b.entries) >= b.capacity
}

// Request returns an io.Reader reading a YAML encoding of the entries
// currently stored in the testBuffer's entries slice.
func (b *testBuffer) Request() (io.Reader, error) {
	req, err := yaml.Marshal(b.entries)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(req), nil
}

func (b *testBuffer) Reset() {
	b.entries = []servicelog.Entry{}
}

// testClient is a "fake" implementation of logClient, for use in testing.
type testClient struct {
	recv chan []servicelog.Entry
}

// Send reads from the provided io.Reader, attempts to decode from YAML to a
// []servicelog.Entry, then sends the decoded value on the recv channel.
func (c testClient) Send(body io.Reader) error {
	entries := []servicelog.Entry{}
	decoder := yaml.NewDecoder(body)
	err := decoder.Decode(&entries)
	if err != nil {
		return err
	}
	c.recv <- entries
	return nil
}
