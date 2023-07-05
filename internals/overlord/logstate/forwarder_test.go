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
	"fmt"
	"time"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/servicelog"
)

type forwarderSuite struct{}

var _ = Suite(&forwarderSuite{})

func (s *forwarderSuite) TestForwarder(c *C) {
	serviceName := "foobar"
	ringBuffer := servicelog.NewRingBuffer(1024)
	logWriter := servicelog.NewFormatWriter(ringBuffer, serviceName)

	recv1, recv2 := make(chan []servicelog.Entry), make(chan []servicelog.Entry)
	gatherer1 := newLogGathererForTest(nil, 1*time.Microsecond, 5, recv1)
	go gatherer1.loop()
	gatherer2 := newLogGathererForTest(nil, 1*time.Microsecond, 5, recv2)
	go gatherer2.loop()

	forwarder := newLogForwarder(serviceName)
	go forwarder.forward(ringBuffer)

	forwarder.mu.Lock()
	forwarder.gatherers = []*logGatherer{gatherer1, gatherer2}
	forwarder.mu.Unlock()

	message := "this is a log line"
	_, err := fmt.Fprintln(logWriter, message)
	c.Assert(err, IsNil)

	select {
	case entries := <-recv1:
		c.Assert(entries, HasLen, 1)
		c.Check(entries[0].Service, Equals, serviceName)
		c.Check(entries[0].Message, Equals, message+"\n")
	case <-time.After(10 * time.Millisecond):
		c.Fatal("timeout waiting to receive logs from gatherer1")
	}

	select {
	case entries := <-recv2:
		c.Assert(entries, HasLen, 1)
		c.Check(entries[0].Service, Equals, serviceName)
		c.Check(entries[0].Message, Equals, message+"\n")
	case <-time.After(10 * time.Millisecond):
		c.Fatal("timeout waiting to receive logs from gatherer2")
	}
}
