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
	"testing"
	"time"

	"github.com/canonical/tc"

	"github.com/canonical/pebble/internals/servicelog"
)

type iteratorSuite struct{}

func TestIteratorSuite(t *testing.T) {
	tc.Run(t, &iteratorSuite{})
}

func (s *iteratorSuite) TestReads(c *tc.C) {
	rb := servicelog.NewRingBuffer(100)
	for range 10 {
		n, err := fmt.Fprint(rb, "0123456789")
		c.Assert(err, tc.ErrorIsNil)
		c.Assert(n, tc.Equals, 10)
	}

	it := rb.TailIterator()
	num := 0
	for it.Next(nil) {
		buf := [10]byte{}
		n, err := it.Read(buf[:])
		if err != nil && err != io.EOF {
			c.Fatalf("read did not return nil or io.EOF")
		}
		c.Assert(n, tc.Equals, 10)
		c.Assert(string(buf[:]), tc.Equals, "0123456789")
		num++
	}

	it.Close()

	c.Assert(num, tc.Equals, 10)
}

func (s *iteratorSuite) TestConcurrentReaders(c *tc.C) {
	rb := servicelog.NewRingBuffer(10000)
	for range 1000 {
		n, err := fmt.Fprintln(rb, "123456789")
		c.Assert(err, tc.ErrorIsNil)
		c.Assert(n, tc.Equals, 10)
	}

	numReaders := max(runtime.NumCPU(), 2)
	start := make(chan struct{})
	wg := sync.WaitGroup{}
	wg.Add(numReaders)
	num := int32(0)
	for range numReaders {
		go func() {
			defer wg.Done()
			<-start
			it := rb.TailIterator()
			localNum := int32(0)
			for it.Next(nil) {
				buf := [10]byte{}
				n, err := it.Read(buf[:])
				if err != nil && err != io.EOF {
					c.Log("read did not return nil or io.EOF")
					c.Fail()
					return
				}
				c.Assert(n, tc.Equals, 10)
				c.Assert(string(buf[:]), tc.Equals, "123456789\n")
				localNum++
			}
			it.Close()
			atomic.AddInt32(&num, localNum)
		}()
	}

	close(start)
	wg.Wait()

	c.Assert(atomic.LoadInt32(&num), tc.Equals, 1000*int32(numReaders))
}

func (s *iteratorSuite) TestMore(c *tc.C) {
	n := 1000
	expectedSum := 0
	for i := range n {
		expectedSum += i
	}

	timeout := make(chan struct{})
	defer time.AfterFunc(10*time.Second, func() {
		close(timeout)
	})

	sendMore := make(chan struct{})
	wg := sync.WaitGroup{}
	rb := servicelog.NewRingBuffer(10000)

	wg.Go(func() {
		for i := range n {
			select {
			case <-sendMore:
			case <-timeout:
				c.Log("timeout waiting to send more")
				c.Fail()
				return
			}
			_, err := fmt.Fprintf(rb, "%d", i)
			c.Assert(err, tc.ErrorIsNil)
		}
	})

	resultSum := 0
	wg.Go(func() {
		defer close(sendMore)
		it := rb.TailIterator()
		defer it.Close()
		for {
			if resultSum == expectedSum {
				break
			}
			// Check there isn't any items.
			ok := it.Next(nil)
			c.Assert(ok, tc.Equals, false)
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
			c.Assert(ok, tc.Equals, true)
			buf := &bytes.Buffer{}
			_, err := io.Copy(buf, it)
			c.Assert(err, tc.ErrorIsNil)
			i, err := strconv.Atoi(buf.String())
			c.Assert(err, tc.ErrorIsNil)
			resultSum += i
		}
	})

	wg.Wait()

	c.Assert(resultSum, tc.Equals, expectedSum)
}

func (s *iteratorSuite) TestWriteTo(c *tc.C) {
	rb := servicelog.NewRingBuffer(1000)
	n0, err := fmt.Fprintln(rb, "testing testing testing")
	c.Assert(err, tc.ErrorIsNil)

	buf := &bytes.Buffer{}
	it := rb.TailIterator()
	for it.Next(nil) {
		n1, err := it.WriteTo(buf)
		c.Assert(err, tc.ErrorIsNil)
		c.Assert(int(n1), tc.Equals, n0)
	}
	err = it.Close()
	c.Assert(err, tc.ErrorIsNil)

	err = rb.Close()
	c.Assert(err, tc.ErrorIsNil)
}

func (s *iteratorSuite) TestLongReaders(c *tc.C) {
	rb := servicelog.NewRingBuffer(1000)

	wg := sync.WaitGroup{}
	readCount := int32(0)
	timeout := make(chan struct{})
	defer time.AfterFunc(10*time.Second, func() {
		close(timeout)
	})
	numReaders := 100
	for range numReaders {
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

	for i := range 10 {
		n, err := fmt.Fprintf(rb, "%d", i)
		c.Assert(err, tc.ErrorIsNil)
		c.Assert(n, tc.Equals, 1)
	}

	err := rb.Close()
	c.Assert(err, tc.ErrorIsNil)
	wg.Wait()

	c.Assert(int(readCount), tc.Equals, 10*numReaders)
	c.Assert(time.Since(startTime) > 10*time.Millisecond, tc.Equals, true)
}

func (s *iteratorSuite) TestTruncation(c *tc.C) {
	rb := servicelog.NewRingBuffer(10)
	iter := rb.TailIterator()
	fmt.Fprint(rb, "0123456789")
	fmt.Fprint(rb, "0123456789")
	buffer := &bytes.Buffer{}
	for iter.Next(nil) {
		_, err := io.Copy(buffer, iter)
		c.Assert(err, tc.ErrorIsNil)
	}
	c.Assert(buffer.String(), tc.Equals, "\n(... output truncated ...)\n0123456789")
}

func (s *iteratorSuite) TestTruncationByteByByte(c *tc.C) {
	rb := servicelog.NewRingBuffer(10)
	iter := rb.TailIterator()
	fmt.Fprint(rb, "0123456789")
	fmt.Fprint(rb, "0123456789")
	buffer := &bytes.Buffer{}
	for iter.Next(nil) {
		one := [1]byte{}
		n, err := iter.Read(one[:])
		if err != nil && err != io.EOF {
			c.Fatalf("read did not return nil or io.EOF")
		}
		c.Assert(n, tc.Equals, 1)
		buffer.WriteByte(one[0])
	}
	c.Assert(buffer.String(), tc.Equals, "\n(... output truncated ...)\n0123456789")
}

func (s *iteratorSuite) TestClosed(c *tc.C) {
	rb := servicelog.NewRingBuffer(10)
	fmt.Fprint(rb, "0123456789")
	iter0 := rb.TailIterator()
	err := rb.Close()
	c.Assert(err, tc.ErrorIsNil)
	iter1 := rb.TailIterator()

	buffer0 := &bytes.Buffer{}
	n, err := iter0.WriteTo(buffer0)
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(n, tc.Equals, int64(10))
	c.Assert(iter0.Close(), tc.IsNil)

	buffer1 := &bytes.Buffer{}
	n, err = iter1.WriteTo(buffer1)
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(n, tc.Equals, int64(10))
	c.Assert(iter1.Close(), tc.IsNil)
}

func (s *iteratorSuite) TestClosedIteration(c *tc.C) {
	rb := servicelog.NewRingBuffer(10)
	fmt.Fprint(rb, "0123456789")
	iter := rb.TailIterator()
	err := rb.Close()
	c.Assert(err, tc.ErrorIsNil)

	done := make(chan struct{})
	buffer := &bytes.Buffer{}
	for iter.Next(done) {
		one := [1]byte{}
		n, err := iter.Read(one[:])
		if err == io.EOF {
			err = nil
		}
		c.Assert(err, tc.ErrorIsNil)
		c.Assert(n, tc.Equals, 1)
		buffer.WriteByte(one[0])
	}
	c.Assert(buffer.String(), tc.Equals, "0123456789")
}

func (s *iteratorSuite) TestHeadIterator(c *tc.C) {
	rb := servicelog.NewRingBuffer(100)
	fmt.Fprint(rb, "first")
	iter := rb.HeadIterator(0)
	fmt.Fprint(rb, "second")

	buffer := &bytes.Buffer{}
	for iter.Next(nil) {
		io.Copy(buffer, iter)
	}

	c.Assert(buffer.String(), tc.Equals, "second")
}

func (s *iteratorSuite) TestHeadIteratorReplayLines(c *tc.C) {
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

	c.Assert(buffer.String(), tc.Equals, "fourth\nfifth\n")
}

func (s *iteratorSuite) TestEOF(c *tc.C) {
	rb := servicelog.NewRingBuffer(200)
	fmt.Fprintln(rb, "first")
	fmt.Fprintln(rb, "second")
	fmt.Fprintln(rb, "third")
	fmt.Fprintln(rb, "fourth")
	fmt.Fprintln(rb, "fifth")

	iter := rb.HeadIterator(2)
	b := iter.Next(nil)
	c.Assert(b, tc.Equals, true)

	a := make([]byte, 200)
	_, err := iter.Read(a)
	c.Assert(err, tc.Equals, io.EOF)

	b = iter.Next(nil)
	c.Assert(b, tc.Equals, false)
}

func (s *iteratorSuite) TestNotify(c *tc.C) {
	notify := make(chan bool, 1)
	rb := servicelog.NewRingBuffer(200)
	defer rb.Close()
	iter := rb.TailIterator()
	defer iter.Close()
	iter.Notify(notify)
	select {
	case <-notify:
		c.Fatal("notify unexpected")
	default:
	}
	fmt.Fprintln(rb, "first")
	select {
	case <-notify:
	default:
		c.Fatal("notify expected")
	}
	fmt.Fprintln(rb, "second")
	select {
	case <-notify:
	default:
		c.Fatal("notify expected")
	}
	select {
	case <-notify:
		c.Fatal("notify unexpected")
	default:
	}
}
