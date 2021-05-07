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

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internal/servicelog"
)

type ringBufferSuite struct{}

var _ = Suite(&ringBufferSuite{})

func (s *ringBufferSuite) TestWrites(c *C) {
	rb := servicelog.NewRingBuffer(10)
	c.Assert(rb.Pos(), Equals, servicelog.RingPos(0))

	n, err := fmt.Fprint(rb, "pebbletron")
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 10)

	c.Assert(rb.Pos(), Equals, servicelog.RingPos(10))
	c.Assert(rb.Available(), Equals, 0)

	n, err = fmt.Fprint(rb, "no write")
	c.Assert(err, Equals, io.ErrShortWrite)
	c.Assert(n, Equals, 0)
}

func (s *ringBufferSuite) TestCrossBoundaryWriteCopy(c *C) {
	rb := servicelog.NewRingBuffer(13)
	a1 := rb.Pos()
	c.Assert(a1, Equals, servicelog.RingPos(0))
	n, err := fmt.Fprint(rb, "pebble")
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 6)
	a2 := rb.Pos()
	c.Assert(a2, Equals, servicelog.RingPos(6))
	c.Assert(rb.Available(), Equals, 7)

	a := make([]byte, 6)
	n, err = rb.Copy(a, a1, a2)
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 6)
	c.Assert(string(a), Equals, "pebble")

	b1 := rb.Pos()
	c.Assert(b1, Equals, servicelog.RingPos(6))
	n, err = fmt.Fprint(rb, "elbbep")
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 6)
	b2 := rb.Pos()
	c.Assert(b2, Equals, servicelog.RingPos(12))
	c.Assert(rb.Available(), Equals, 1)

	b := make([]byte, 6)
	n, err = rb.Copy(b, b1, b2)
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 6)
	c.Assert(string(b), Equals, "elbbep")

	err = rb.Discard(a1, a2)
	c.Assert(err, IsNil)
	c.Assert(rb.Available(), Equals, 7)

	c1 := rb.Pos()
	c.Assert(c1, Equals, servicelog.RingPos(12))
	n, err = fmt.Fprint(rb, "PEBBLE")
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 6)
	c2 := rb.Pos()
	c.Assert(c2, Equals, servicelog.RingPos(18))
	c.Assert(rb.Available(), Equals, 1)

	cc := make([]byte, 6)
	n, err = rb.Copy(cc, c1, c2)
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 6)
	c.Assert(string(cc), Equals, "PEBBLE")
}

func (s *ringBufferSuite) TestCrossBoundaryWriteWithWriteTo(c *C) {
	rb := servicelog.NewRingBuffer(13)
	a1 := rb.Pos()
	c.Assert(a1, Equals, servicelog.RingPos(0))
	n, err := fmt.Fprint(rb, "pebble")
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 6)
	a2 := rb.Pos()
	c.Assert(a2, Equals, servicelog.RingPos(6))
	c.Assert(rb.Available(), Equals, 7)

	a := &bytes.Buffer{}
	read, err := rb.WriteTo(a, a1, a2)
	c.Assert(err, IsNil)
	c.Assert(read, Equals, int64(6))
	c.Assert(a.String(), Equals, "pebble")

	b1 := rb.Pos()
	c.Assert(b1, Equals, servicelog.RingPos(6))
	n, err = fmt.Fprint(rb, "elbbep")
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 6)
	b2 := rb.Pos()
	c.Assert(b2, Equals, servicelog.RingPos(12))
	c.Assert(rb.Available(), Equals, 1)

	b := &bytes.Buffer{}
	read, err = rb.WriteTo(b, b1, b2)
	c.Assert(err, IsNil)
	c.Assert(read, Equals, int64(6))
	c.Assert(b.String(), Equals, "elbbep")

	err = rb.Discard(a1, a2)
	c.Assert(err, IsNil)
	c.Assert(rb.Available(), Equals, 7)

	c1 := rb.Pos()
	c.Assert(c1, Equals, servicelog.RingPos(12))
	n, err = fmt.Fprint(rb, "PEBBLE")
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 6)
	c2 := rb.Pos()
	c.Assert(c2, Equals, servicelog.RingPos(18))
	c.Assert(rb.Available(), Equals, 1)

	cc := &bytes.Buffer{}
	read, err = rb.WriteTo(cc, c1, c2)
	c.Assert(err, IsNil)
	c.Assert(read, Equals, int64(6))
	c.Assert(cc.String(), Equals, "PEBBLE")
}

func (s *ringBufferSuite) TestWriteShort(c *C) {
	rb := servicelog.NewRingBuffer(1)
	n, err := fmt.Fprint(rb, "ab")
	c.Assert(err, Equals, io.ErrShortWrite)
	c.Assert(n, Equals, 1)
}

func (s *ringBufferSuite) TestReleaseOutOfOrder(c *C) {
	rb := servicelog.NewRingBuffer(2)
	n, err := fmt.Fprint(rb, "ab")
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 2)

	err = rb.Discard(1, 2)
	c.Assert(err, Equals, servicelog.ErrFreeOutOfOrder)
}

func (s *ringBufferSuite) TestReleaseOutOfRange(c *C) {
	rb := servicelog.NewRingBuffer(3)
	n, err := fmt.Fprint(rb, "abc")
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 3)

	err = rb.Discard(0, 1)
	c.Assert(err, IsNil)

	err = rb.Discard(0, 1)
	c.Assert(err, Equals, servicelog.ErrOutOfRange)

	err = rb.Discard(3, 4)
	c.Assert(err, Equals, servicelog.ErrOutOfRange)
}

func (s *ringBufferSuite) TestCopy(c *C) {
	rb := servicelog.NewRingBuffer(3)
	n, err := fmt.Fprint(rb, "abc")
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 3)

	a := make([]byte, 3)
	n, err = rb.Copy(a, 0, 3)
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 3)
	c.Assert(string(a), Equals, "abc")

	b := make([]byte, 1)
	n, err = rb.Copy(b, 0, 1)
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 1)
	c.Assert(string(b), Equals, "a")

	cc := make([]byte, 1)
	n, err = rb.Copy(cc, 0, 3)
	c.Assert(err, Equals, io.ErrShortBuffer)
	c.Assert(n, Equals, 1)
	c.Assert(string(cc), Equals, "a")
}

func (s *ringBufferSuite) TestFullWrite(c *C) {
	rb := servicelog.NewRingBuffer(10)
	p1 := rb.Pos()
	n, err := fmt.Fprintf(rb, "0123456789")
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 10)
	p2 := rb.Pos()

	slice := make([]byte, 10)
	n, err = rb.Copy(slice, p1, p2)
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 10)
	c.Assert(string(slice), Equals, "0123456789")

	buffer := &bytes.Buffer{}
	n1, err := rb.WriteTo(buffer, p1, p2)
	c.Assert(err, IsNil)
	c.Assert(n1, Equals, int64(10))
	c.Assert(buffer.String(), Equals, "0123456789")
}

func (s *ringBufferSuite) TestFullWriteCrossBoundary(c *C) {
	rb := servicelog.NewRingBuffer(10)
	p1 := rb.Pos()
	n, err := fmt.Fprintf(rb, "01234")
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 5)
	p2 := rb.Pos()
	err = rb.Discard(p1, p2)
	c.Assert(err, IsNil)

	p1 = rb.Pos()
	c.Assert(p1, Not(Equals), servicelog.RingPos(0))
	n, err = fmt.Fprintf(rb, "0123456789")
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 10)
	p2 = rb.Pos()

	slice := make([]byte, 10)
	n, err = rb.Copy(slice, p1, p2)
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 10)
	c.Assert(string(slice), Equals, "0123456789")

	buffer := &bytes.Buffer{}
	n1, err := rb.WriteTo(buffer, p1, p2)
	c.Assert(err, IsNil)
	c.Assert(n1, Equals, int64(10))
	c.Assert(buffer.String(), Equals, "0123456789")

	buffers := rb.Buffers(p1, p2)
	c.Assert(len(buffers), Equals, 2)
	combined := append(append([]byte(nil), buffers[0]...), buffers[1]...)
	c.Assert(string(combined), Equals, "0123456789")
}

func (s *ringBufferSuite) TestAllocs(c *C) {
	rb := servicelog.NewRingBuffer(10)
	payload := []byte("0123456789")
	numAllocs := testing.AllocsPerRun(32, func() {
		p1 := rb.Pos()
		_, err := rb.Write(payload)
		if err != nil {
			// this looks funny, but its to avoid allocs.
			c.Assert(err, IsNil)
		}
		p2 := rb.Pos()
		err = rb.Discard(p1, p2)
		if err != nil {
			// this looks funny, but its to avoid allocs.
			c.Assert(err, IsNil)
		}
	})
	c.Assert(int(numAllocs), Equals, 0)
}
