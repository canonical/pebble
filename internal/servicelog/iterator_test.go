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
	wb := servicelog.NewWriteBuffer(10, 100)
	stdout := wb.StreamWriter(servicelog.Stdout)
	for i := 0; i < 10; i++ {
		n, err := fmt.Fprint(stdout, "0123456789")
		c.Assert(err, IsNil)
		c.Assert(n, Equals, 10)
	}

	it := wb.TailIterator()
	num := 0
	for it.Next(nil) {
		c.Assert(it.Length(), Equals, 10)
		c.Assert(it.StreamID(), Equals, servicelog.Stdout)
		c.Assert(it.Timestamp(), Not(Equals), time.Time{})
		buf := [10]byte{}
		n, err := it.Read(buf[:])
		c.Assert(err, IsNil)
		c.Assert(n, Equals, 10)
		c.Assert(buf[:], DeepEquals, []byte("0123456789"))
		num++
	}

	it.Close()

	c.Assert(num, Equals, 10)
}

func (s *iteratorSuite) TestConcurrentReaders(c *C) {
	wb := servicelog.NewWriteBuffer(1000, 10000)
	stdout := wb.StreamWriter(servicelog.Stdout)
	for i := 0; i < 1000; i++ {
		n, err := fmt.Fprintln(stdout, "123456789")
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
			it := wb.TailIterator()
			localNum := int32(0)
			for it.Next(nil) {
				c.Assert(it.Length(), Equals, 10)
				c.Assert(it.StreamID(), Equals, servicelog.Stdout)
				c.Assert(it.Timestamp(), Not(Equals), time.Time{})
				buf := [10]byte{}
				n, err := it.Read(buf[:])
				c.Assert(err, IsNil)
				c.Assert(n, Equals, 10)
				c.Assert(buf[:], DeepEquals, []byte("123456789\n"))
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
	wb := servicelog.NewWriteBuffer(1000, 10000)

	stdout := wb.StreamWriter(servicelog.Stdout)
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
			_, err := fmt.Fprintf(stdout, "%d", i)
			c.Assert(err, IsNil)
		}
	}()

	resultSum := 0
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(sendMore)
		it := wb.TailIterator()
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
			buf := make([]byte, it.Length())
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
	wb := servicelog.NewWriteBuffer(100, 1000)
	stdout := wb.StreamWriter(servicelog.Stdout)
	n0, err := fmt.Fprintln(stdout, "testing testing testing")
	c.Assert(err, IsNil)

	buf := &bytes.Buffer{}
	it := wb.TailIterator()
	for it.Next(nil) {
		n1, err := it.WriteTo(buf)
		c.Assert(err, IsNil)
		c.Assert(int(n1), Equals, n0)
	}
	err = it.Close()
	c.Assert(err, IsNil)

	err = wb.Close()
	c.Assert(err, IsNil)
}

func (s *iteratorSuite) TestResetRead(c *C) {
	testStr := "testing testing testing"
	wb := servicelog.NewWriteBuffer(100, 1000)
	stdout := wb.StreamWriter(servicelog.Stdout)
	n0, err := fmt.Fprint(stdout, testStr)
	c.Assert(err, IsNil)

	it := wb.TailIterator()
	c.Assert(it.Next(nil), Equals, true)
	for i := 0; i < 2; i++ {
		buf := &bytes.Buffer{}
		n1, err := it.WriteTo(buf)
		c.Assert(err, IsNil)
		c.Assert(int(n1), Equals, n0)
		c.Assert(buf.String(), Equals, testStr)
		it.Reset()
	}

	err = it.Close()
	c.Assert(err, IsNil)

	err = wb.Close()
	c.Assert(err, IsNil)
}

func (s *iteratorSuite) TestLongReaders(c *C) {
	wb := servicelog.NewWriteBuffer(5, 1000)
	stdout := wb.StreamWriter(servicelog.Stdout)

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
		it := wb.TailIterator()
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
		n, err := fmt.Fprintf(stdout, "%d", i)
		c.Assert(err, IsNil)
		c.Assert(n, Equals, 1)
	}

	err := wb.Close()
	c.Assert(err, IsNil)
	wg.Wait()

	c.Assert(int(readCount), Equals, 10*numReaders)
	c.Assert(time.Since(startTime) > 10*time.Millisecond, Equals, true)
}
