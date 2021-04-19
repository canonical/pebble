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
	"fmt"
	"io"
	"sync"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internal/servicelog"
)

type writeBufferSuite struct{}

var _ = Suite(&writeBufferSuite{})

func (s *writeBufferSuite) TestWrites(c *C) {
	wb := servicelog.NewWriteBuffer(10, 100)
	stdout := wb.StreamWriter(servicelog.Stdout)
	for i := 0; i < 1000; i++ {
		n, err := fmt.Fprintln(stdout, "123456789")
		c.Assert(err, IsNil)
		c.Assert(n, Equals, 10)
	}
	err := wb.Close()
	c.Assert(err, IsNil)
}

func (s *writeBufferSuite) TestConcurrentWrites(c *C) {
	wb := servicelog.NewWriteBuffer(10, 100)
	writers := []io.Writer{wb.StreamWriter(servicelog.Stdout), wb.StreamWriter(servicelog.Stderr)}
	wg := sync.WaitGroup{}
	wg.Add(len(writers))
	for _, v := range writers {
		writer := v
		go func() {
			defer wg.Done()
			for i := 0; i < 10000; i++ {
				n, err := fmt.Fprintln(writer, "123456789")
				c.Assert(err, IsNil)
				c.Assert(n, Equals, 10)
			}
		}()
	}
	wg.Wait()
	err := wb.Close()
	c.Assert(err, IsNil)
}

func (s *writeBufferSuite) TestWritePatterns(c *C) {
	tests := []struct {
		MaxWrites  int
		BufferSize int
		Stride     int
		Iterations int
	}{
		{10, 10, 1, 20},
		{10, 10, 2, 20},
		{10, 10, 3, 20},
		{10, 15, 1, 20},
		{10, 15, 2, 20},
		{10, 15, 3, 20},
		{5, 10, 1, 20},
		{5, 10, 2, 20},
		{5, 10, 3, 20},
		{5, 15, 1, 20},
		{5, 15, 2, 20},
		{5, 15, 3, 20},
	}
	for t, test := range tests {
		wb := servicelog.NewWriteBuffer(10, test.BufferSize)
		payload := make([]byte, test.Stride)
		for i := 0; i < test.Iterations; i++ {
			n, err := wb.Write(payload, servicelog.Stdout)
			c.Assert(err, IsNil, Commentf("test #%d iter #%d", t, i))
			c.Assert(n, Equals, test.Stride, Commentf("test #%d iter #%d", t, i))
		}
		err := wb.Close()
		c.Assert(err, IsNil)
	}
}

func (s *writeBufferSuite) TestWriteOversize(c *C) {
	wb := servicelog.NewWriteBuffer(2, 10)
	n, err := wb.Write([]byte("0123456789overflow"), servicelog.Stdout)
	c.Assert(err, Equals, io.ErrShortWrite)
	c.Assert(n, Equals, 10)
}

func (s *writeBufferSuite) TestAllocs(c *C) {
	wb := servicelog.NewWriteBuffer(100, 1000)
	payload := []byte("0123456789")
	numAllocs := testing.AllocsPerRun(32, func() {
		_, err := wb.Write(payload, servicelog.Stdout)
		if err != nil {
			// this looks funny, but its to avoid allocs.
			c.Assert(err, IsNil)
		}
	})
	// Expect 1 allocation for the notification channel only.
	c.Assert(int(numAllocs), Equals, 1)
	err := wb.Close()
	c.Assert(err, IsNil)
}
