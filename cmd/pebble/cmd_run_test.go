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
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package main_test

import (
	"bufio"
	"bytes"
	"fmt"
	"strings"
	"sync"
	"time"

	"gopkg.in/check.v1"

	pebble "github.com/canonical/pebble/cmd/pebble"
	"github.com/canonical/pebble/internal/servicelog"
)

func (s *PebbleSuite) TestLogWriterSimple(c *check.C) {
	w := &bytes.Buffer{}
	lw := pebble.LogWriter{Writer: w}

	err := lw.WriteLog(
		time.Date(2021, 8, 4, 2, 3, 4, 0, time.UTC),
		"nginx",
		servicelog.Stdout,
		strings.NewReader("this is a test\n"),
	)
	c.Assert(err, check.IsNil)
	c.Assert(w.String(), check.Equals, "2021-08-04T02:03:04Z nginx stdout: this is a test\n")

	w.Reset()
	err = lw.WriteLog(
		time.Date(2021, 12, 25, 12, 23, 34, 456789, time.UTC),
		"postgresql",
		servicelog.Stderr,
		strings.NewReader("some kind of error\n"),
	)
	c.Assert(err, check.IsNil)
	c.Assert(w.String(), check.Equals, "2021-12-25T12:23:34Z postgresql stderr: some kind of error\n")
}

func (s *PebbleSuite) TestLogWriterConcurrent(c *check.C) {
	w := &bytes.Buffer{}
	lw := pebble.LogWriter{Writer: w}

	// Fire up a couple of concurrent goroutines writing logs.
	var wg sync.WaitGroup
	for n := 0; n < 2; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				err := lw.WriteLog(
					time.Date(2021, 8, 4, 2, 3, 4, 0, time.UTC),
					"nginx",
					servicelog.Stdout,
					strings.NewReader(fmt.Sprintf("message %d\n", i)),
				)
				c.Assert(err, check.IsNil)
			}
		}()
	}
	wg.Wait()

	// Ensure that locking is working: timestamp will be at the start of each
	// line, and the buffer is being locked correctly.
	scanner := bufio.NewScanner(w)
	for scanner.Scan() {
		c.Assert(scanner.Text(), check.Matches, `2021-08-04T02:03:04Z nginx stdout: message \d+`)
	}
	c.Assert(scanner.Err(), check.IsNil)
}
