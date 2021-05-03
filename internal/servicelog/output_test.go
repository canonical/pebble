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
	"bytes"
	"fmt"
	"io"
	"time"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internal/servicelog"
)

type outputSuite struct{}

var _ = Suite(&outputSuite{})

func (s *outputSuite) TestSink(c *C) {
	numMessages := 10
	wb := servicelog.NewWriteBuffer(numMessages, 100)
	defer wb.Close()
	stdout := wb.StreamWriter(servicelog.Stdout)
	logStart := time.Now()
	for i := 0; i < numMessages; i++ {
		n, err := fmt.Fprint(stdout, "0123456789")
		c.Assert(err, IsNil)
		c.Assert(n, Equals, 10)
	}
	logEnd := time.Now()

	it := wb.TailIterator()
	defer it.Close()

	readAll := make(chan struct{})
	readCount := 0
	lastTimestamp := logStart
	output := servicelog.OutputFunc(func(timestamp time.Time, serviceName string, stream servicelog.StreamID, length int, message io.Reader) error {
		if timestamp.Before(logStart) {
			c.Fatalf("log ts %v happened before start", timestamp)
		}
		if timestamp.After(logEnd) {
			c.Fatalf("log ts %v happened after end", timestamp)
		}
		if timestamp.Before(lastTimestamp) {
			c.Fatalf("log ts %v out of order", timestamp)
		}

		c.Assert(serviceName, Equals, "test")
		c.Assert(stream, Equals, servicelog.Stdout)

		buffer := &bytes.Buffer{}
		_, err := io.Copy(buffer, message)
		if err != nil {
			return err
		}
		c.Assert(buffer.String(), Equals, "0123456789")

		readCount++
		if readCount == numMessages {
			close(readAll)
		}

		return nil
	})

	done := make(chan struct{})
	go func() {
		select {
		case <-time.After(10 * time.Millisecond):
			c.Log("timeout waiting for all messages")
			c.Fail()
		case <-readAll:
			close(done)
		}
	}()

	err := servicelog.Sink(it, output, "test", done)
	c.Assert(err, IsNil)
}
