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
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internal/servicelog"
)

type iteratorSuite struct{}

var _ = Suite(&iteratorSuite{})

func (s *iteratorSuite) TestReads(c *C) {
	rb := servicelog.NewRingBuffer(100)
	for i := 0; i < 10; i++ {
		n, err := fmt.Fprint(rb, "0123456789")
		c.Assert(err, IsNil)
		c.Assert(n, Equals, 10)
	}

	it := rb.TailIterator()
	num := 0
	for it.Next(nil) {
		buf := [10]byte{}
		n, err := it.Read(buf[:])
		c.Assert(err, IsNil)
		c.Assert(n, Equals, 10)
		c.Assert(string(buf[:]), Equals, "0123456789")
		num++
	}

	it.Close()

	c.Assert(num, Equals, 10)
}

func (s *iteratorSuite) TestConcurrentReaders(c *C) {
	rb := servicelog.NewRingBuffer(10000)
	for i := 0; i < 1000; i++ {
		n, err := fmt.Fprintln(rb, "123456789")
		c.Assert(err, IsNil)
		c.Assert(n, Equals, 10)
	}

	numReaders := runtime.NumCPU()
	if numReaders < 2 {
		numReaders = 2
	}
	start := make(chan struct{})
	wg := sync.WaitGroup{}
	wg.Add(numReaders)
	num := int32(0)
	for i := 0; i < numReaders; i++ {
		go func() {
			defer wg.Done()
			<-start
			it := rb.TailIterator()
			localNum := int32(0)
			for it.Next(nil) {
				buf := [10]byte{}
				n, err := it.Read(buf[:])
				c.Assert(err, IsNil)
				c.Assert(n, Equals, 10)
				c.Assert(string(buf[:]), Equals, "123456789\n")
				localNum++
			}
			it.Close()
			atomic.AddInt32(&num, localNum)
		}()
	}

	close(start)
	wg.Wait()

	c.Assert(atomic.LoadInt32(&num), Equals, 1000*int32(numReaders))
}

func (s *iteratorSuite) TestMore(c *C) {
	n := 1000
	expectedSum := 0
	for i := 0; i < n; i++ {
		expectedSum += i
	}

	timeout := make(chan struct{})
	defer time.AfterFunc(10*time.Second, func() {
		close(timeout)
	})

	sendMore := make(chan struct{})
	wg := sync.WaitGroup{}
	rb := servicelog.NewRingBuffer(10000)

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			select {
			case <-sendMore:
			case <-timeout:
				c.Log("timeout waiting to send more")
				c.Fail()
				return
			}
			_, err := fmt.Fprintf(rb, "%d", i)
			c.Assert(err, IsNil)
		}
	}()

	resultSum := 0
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(sendMore)
		it := rb.TailIterator()
		defer it.Close()
		for {
			if resultSum == expectedSum {
				break
			}
			// Check there isn't any items.
			ok := it.Next(nil)
			c.Assert(ok, Equals, false)
			go func() {
				time.Sleep(500 * time.Microsecond)
				// Ask for annother write.
				select {
				case sendMore <- struct{}{}:
				case <-timeout:
					c.Log("timeout sending")
					c.Fail()
				}
			}()
			// Wait for write.
			ok = it.Next(timeout)
			c.Assert(ok, Equals, true)
			buf := make([]byte, it.Buffered())
			_, err := it.Read(buf)
			c.Assert(err, IsNil)
			i, err := strconv.Atoi(string(buf))
			c.Assert(err, IsNil)
			resultSum += i
		}
	}()

	wg.Wait()

	c.Assert(resultSum, Equals, expectedSum)
}

func (s *iteratorSuite) TestWriteTo(c *C) {
	rb := servicelog.NewRingBuffer(1000)
	n0, err := fmt.Fprintln(rb, "testing testing testing")
	c.Assert(err, IsNil)

	buf := &bytes.Buffer{}
	it := rb.TailIterator()
	for it.Next(nil) {
		n1, err := it.WriteTo(buf)
		c.Assert(err, IsNil)
		c.Assert(int(n1), Equals, n0)
	}
	err = it.Close()
	c.Assert(err, IsNil)

	err = rb.Close()
	c.Assert(err, IsNil)
}

func (s *iteratorSuite) TestLongReaders(c *C) {
	rb := servicelog.NewRingBuffer(1000)

	wg := sync.WaitGroup{}
	readCount := int32(0)
	timeout := make(chan struct{})
	defer time.AfterFunc(10*time.Second, func() {
		close(timeout)
	})
	numReaders := 100
	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		// Iterator needs to be created before the writes start
		// as to not miss them.
		it := rb.TailIterator()
		go func() {
			defer wg.Done()
			defer it.Close()
			localRead := int32(0)
			defer func() {
				atomic.AddInt32(&readCount, localRead)
			}()
			for localRead < 10 && it.Next(timeout) {
				localRead++
				time.Sleep(1 * time.Millisecond)
			}
		}()
	}

	startTime := time.Now()

	for i := 0; i < 10; i++ {
		n, err := fmt.Fprintf(rb, "%d", i)
		c.Assert(err, IsNil)
		c.Assert(n, Equals, 1)
	}

	err := rb.Close()
	c.Assert(err, IsNil)
	wg.Wait()

	c.Assert(int(readCount), Equals, 10*numReaders)
	c.Assert(time.Since(startTime) > 10*time.Millisecond, Equals, true)
}

func (s *iteratorSuite) TestTruncation(c *C) {
	rb := servicelog.NewRingBuffer(10)
	iter := rb.TailIterator()
	fmt.Fprint(rb, "0123456789")
	fmt.Fprint(rb, "0123456789")
	buffer := &bytes.Buffer{}
	for iter.Next(nil) {
		_, err := io.Copy(buffer, iter)
		c.Assert(err, IsNil)
	}
	c.Assert(buffer.String(), Equals, "(... output truncated ...)\n0123456789")
}

func (s *iteratorSuite) TestTruncationByteByByte(c *C) {
	rb := servicelog.NewRingBuffer(10)
	iter := rb.TailIterator()
	fmt.Fprint(rb, "0123456789")
	fmt.Fprint(rb, "0123456789")
	buffer := &bytes.Buffer{}
	for iter.Next(nil) {
		one := [1]byte{}
		n, err := iter.Read(one[:])
		c.Assert(err, IsNil)
		c.Assert(n, Equals, 1)
		buffer.WriteByte(one[0])
	}
	c.Assert(buffer.String(), Equals, "(... output truncated ...)\n0123456789")
}

func (s *iteratorSuite) TestClosed(c *C) {
	rb := servicelog.NewRingBuffer(10)
	fmt.Fprint(rb, "0123456789")
	iter0 := rb.TailIterator()
	err := rb.Close()
	c.Assert(err, IsNil)
	iter1 := rb.TailIterator()

	buffer0 := &bytes.Buffer{}
	n, err := iter0.WriteTo(buffer0)
	c.Assert(err, Equals, io.EOF)
	c.Assert(n, Equals, int64(10))

	buffer1 := &bytes.Buffer{}
	n, err = iter1.WriteTo(buffer1)
	c.Assert(err, Equals, io.EOF)
	c.Assert(n, Equals, int64(10))
}

func (s *iteratorSuite) TestClosedIteration(c *C) {
	rb := servicelog.NewRingBuffer(10)
	fmt.Fprint(rb, "0123456789")
	iter := rb.TailIterator()
	err := rb.Close()
	c.Assert(err, IsNil)

	done := make(chan struct{})
	buffer := &bytes.Buffer{}
	for iter.Next(done) {
		one := [1]byte{}
		n, err := iter.Read(one[:])
		if err == io.EOF {
			err = nil
		}
		c.Assert(err, IsNil)
		c.Assert(n, Equals, 1)
		buffer.WriteByte(one[0])
	}
	c.Assert(buffer.String(), Equals, "0123456789")
}

func (s *iteratorSuite) TestHeadIterator(c *C) {
	rb := servicelog.NewRingBuffer(100)
	fmt.Fprint(rb, "first")
	iter := rb.HeadIterator(0)
	fmt.Fprint(rb, "second")

	buffer := &bytes.Buffer{}
	for iter.Next(nil) {
		io.Copy(buffer, iter)
	}

	c.Assert(buffer.String(), Equals, "second")
}

func (s *iteratorSuite) TestHeadIteratorReplayLines(c *C) {
	rb := servicelog.NewRingBuffer(200)
	fmt.Fprintln(rb, "first")
	fmt.Fprintln(rb, "second")
	fmt.Fprintln(rb, "third")
	fmt.Fprintln(rb, "fourth")
	fmt.Fprintln(rb, "fifth")

	iter := rb.HeadIterator(2)

	buffer := &bytes.Buffer{}
	for iter.Next(nil) {
		io.Copy(buffer, iter)
	}

	c.Assert(buffer.String(), Equals, "fourth\nfifth\n")
}
