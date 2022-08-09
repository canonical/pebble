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
	"testing"

	"github.com/canonical/pebble/internal/servicelog"
	. "gopkg.in/check.v1"
)

type ringBufferSuite struct{}

var _ = Suite(&ringBufferSuite{})

func (s *ringBufferSuite) TestWrites(c *C) {
	rb := servicelog.NewRingBuffer(10)

	n, err := fmt.Fprint(rb, "pebbletron")
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 10)

	p1, p2 := rb.Positions()
	c.Assert(p1, Equals, servicelog.RingPos(0))
	c.Assert(p2, Equals, servicelog.RingPos(10))
	c.Assert(rb.Available(), Equals, 0)
}

func (s *ringBufferSuite) TestCrossBoundaryWriteCopy(c *C) {
	rb := servicelog.NewRingBuffer(13)
	_, a1 := rb.Positions()
	c.Assert(a1, Equals, servicelog.RingPos(0))
	n, err := fmt.Fprint(rb, "pebble")
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 6)
	_, a2 := rb.Positions()
	c.Assert(a2, Equals, servicelog.RingPos(6))
	c.Assert(rb.Available(), Equals, 7)

	a := make([]byte, 6)
	next, n, err := rb.Copy(a, a1)
	c.Assert(err, Equals, io.EOF)
	c.Assert(n, Equals, 6)
	c.Assert(next, Equals, servicelog.RingPos(6))
	c.Assert(string(a), Equals, "pebble")

	_, b1 := rb.Positions()
	c.Assert(b1, Equals, servicelog.RingPos(6))
	n, err = fmt.Fprint(rb, "elbbep")
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 6)
	_, b2 := rb.Positions()
	c.Assert(b2, Equals, servicelog.RingPos(12))
	c.Assert(rb.Available(), Equals, 1)

	b := make([]byte, 6)
	next, n, err = rb.Copy(b, b1)
	c.Assert(err, Equals, io.EOF)
	c.Assert(n, Equals, 6)
	c.Assert(next, Equals, servicelog.RingPos(12))
	c.Assert(string(b), Equals, "elbbep")

	err = rb.Discard(int(a2 - a1))
	c.Assert(err, IsNil)
	c.Assert(rb.Available(), Equals, 7)

	_, c1 := rb.Positions()
	c.Assert(c1, Equals, servicelog.RingPos(12))
	n, err = fmt.Fprint(rb, "PEBBLE")
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 6)
	_, c2 := rb.Positions()
	c.Assert(c2, Equals, servicelog.RingPos(18))
	c.Assert(rb.Available(), Equals, 1)

	cc := make([]byte, 6)
	next, n, err = rb.Copy(cc, c1)
	c.Assert(err, Equals, io.EOF)
	c.Assert(n, Equals, 6)
	c.Assert(next, Equals, servicelog.RingPos(18))
	c.Assert(string(cc), Equals, "PEBBLE")
}

func (s *ringBufferSuite) TestCrossBoundaryWriteAtomicDiscard(c *C) {
	buf := servicelog.NewRingBuffer(10)

	io.WriteString(buf, "hello ")
	io.WriteString(buf, "world ")
	buf.Close()

	result := make([]byte, 12)
	_, n, err := buf.Copy(result, servicelog.TailPosition)
	c.Assert(err, Equals, io.EOF)
	c.Assert(n, Equals, 6)
	// Make sure we discarded the entire first write and only have the second remaining.
	c.Assert(string(result[:n]), Equals, "world ")
}

func (s *ringBufferSuite) TestCrossBoundaryWriteWithWriteTo(c *C) {
	rb := servicelog.NewRingBuffer(13)
	_, a1 := rb.Positions()
	c.Assert(a1, Equals, servicelog.RingPos(0))
	n, err := fmt.Fprint(rb, "pebble")
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 6)
	_, a2 := rb.Positions()
	c.Assert(a2, Equals, servicelog.RingPos(6))
	c.Assert(rb.Available(), Equals, 7)

	a := &bytes.Buffer{}
	next, read, err := rb.WriteTo(a, a1)
	c.Assert(err, IsNil)
	c.Assert(read, Equals, int64(6))
	c.Assert(next, Equals, servicelog.RingPos(6))
	c.Assert(a.String(), Equals, "pebble")

	_, b1 := rb.Positions()
	c.Assert(b1, Equals, servicelog.RingPos(6))
	n, err = fmt.Fprint(rb, "elbbep")
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 6)
	_, b2 := rb.Positions()
	c.Assert(b2, Equals, servicelog.RingPos(12))
	c.Assert(rb.Available(), Equals, 1)

	b := &bytes.Buffer{}
	next, read, err = rb.WriteTo(b, b1)
	c.Assert(err, IsNil)
	c.Assert(read, Equals, int64(6))
	c.Assert(next, Equals, servicelog.RingPos(12))
	c.Assert(b.String(), Equals, "elbbep")

	err = rb.Discard(int(a2 - a1))
	c.Assert(err, IsNil)
	c.Assert(rb.Available(), Equals, 7)

	_, c1 := rb.Positions()
	c.Assert(c1, Equals, servicelog.RingPos(12))
	n, err = fmt.Fprint(rb, "PEBBLE")
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 6)
	_, c2 := rb.Positions()
	c.Assert(c2, Equals, servicelog.RingPos(18))
	c.Assert(rb.Available(), Equals, 1)

	cc := &bytes.Buffer{}
	next, read, err = rb.WriteTo(cc, c1)
	c.Assert(err, IsNil)
	c.Assert(read, Equals, int64(6))
	c.Assert(next, Equals, servicelog.RingPos(18))
	c.Assert(cc.String(), Equals, "PEBBLE")
}

func (s *ringBufferSuite) TestWriteShort(c *C) {
	rb := servicelog.NewRingBuffer(1)
	n, err := fmt.Fprint(rb, "ab")
	c.Assert(err, Equals, io.ErrShortWrite)
	c.Assert(n, Equals, 1)
}

func (s *ringBufferSuite) TestCopy(c *C) {
	rb := servicelog.NewRingBuffer(3)
	n, err := fmt.Fprint(rb, "abc")
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 3)

	a := make([]byte, 3)
	next, n, err := rb.Copy(a, 0)
	c.Assert(err, Equals, io.EOF)
	c.Assert(n, Equals, 3)
	c.Assert(next, Equals, servicelog.RingPos(3))
	c.Assert(string(a), Equals, "abc")

	b := make([]byte, 1)
	next, n, err = rb.Copy(b, 0)
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 1)
	c.Assert(next, Equals, servicelog.RingPos(1))
	c.Assert(string(b), Equals, "a")
}

func (s *ringBufferSuite) TestFullWrite(c *C) {
	rb := servicelog.NewRingBuffer(10)
	_, p1 := rb.Positions()
	n, err := fmt.Fprintf(rb, "0123456789")
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 10)
	_, p2 := rb.Positions()
	c.Assert(p2, Equals, servicelog.RingPos(10))

	slice := make([]byte, 10)
	next, n, err := rb.Copy(slice, p1)
	c.Assert(err, Equals, io.EOF)
	c.Assert(n, Equals, 10)
	c.Assert(next, Equals, servicelog.RingPos(10))
	c.Assert(string(slice), Equals, "0123456789")

	buffer := &bytes.Buffer{}
	next, n1, err := rb.WriteTo(buffer, p1)
	c.Assert(err, IsNil)
	c.Assert(n1, Equals, int64(10))
	c.Assert(next, Equals, servicelog.RingPos(10))
	c.Assert(buffer.String(), Equals, "0123456789")
}

func (s *ringBufferSuite) TestFullWriteCrossBoundary(c *C) {
	rb := servicelog.NewRingBuffer(10)
	_, p1 := rb.Positions()
	n, err := fmt.Fprintf(rb, "01234")
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 5)
	_, p2 := rb.Positions()
	err = rb.Discard(int(p2 - p1))
	c.Assert(err, IsNil)

	_, p1 = rb.Positions()
	c.Assert(p1, Not(Equals), servicelog.RingPos(0))
	n, err = fmt.Fprintf(rb, "0123456789")
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 10)

	slice := make([]byte, 10)
	next, n, err := rb.Copy(slice, p1)
	c.Assert(err, Equals, io.EOF)
	c.Assert(n, Equals, 10)
	c.Assert(next, Equals, servicelog.RingPos(15))
	c.Assert(string(slice), Equals, "0123456789")

	buffer := &bytes.Buffer{}
	next, n1, err := rb.WriteTo(buffer, p1)
	c.Assert(err, IsNil)
	c.Assert(n1, Equals, int64(10))
	c.Assert(next, Equals, servicelog.RingPos(15))
	c.Assert(buffer.String(), Equals, "0123456789")
}

func (s *ringBufferSuite) TestAllocs(c *C) {
	rb := servicelog.NewRingBuffer(10)
	payload := []byte("0123456789")
	numAllocs := testing.AllocsPerRun(32, func() {
		_, p1 := rb.Positions()
		_, err := rb.Write(payload)
		if err != nil {
			// this looks funny, but its to avoid allocs.
			c.Assert(err, IsNil)
		}
		_, p2 := rb.Positions()
		err = rb.Discard(int(p2 - p1))
		if err != nil {
			// this looks funny, but its to avoid allocs.
			c.Assert(err, IsNil)
		}
	})
	c.Assert(int(numAllocs), Equals, 0)
}
