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
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/canonical/tc"

	"github.com/canonical/pebble/internals/servicelog"
)

type parserSuite struct{}

func TestParserSuite(t *testing.T) {
	tc.Run(t, &parserSuite{})
}

func (s *parserSuite) TestParse(c *tc.C) {
	_, err := servicelog.Parse([]byte("foo bar"))
	c.Check(err, tc.ErrorMatches, "line has too few fields")

	_, err = servicelog.Parse([]byte("foo bar baz"))
	c.Check(err, tc.ErrorMatches, "invalid log timestamp")

	_, err = servicelog.Parse([]byte("2021-05-26T12:37:00Z [] baz"))
	c.Check(err, tc.ErrorMatches, "invalid log service name")

	_, err = servicelog.Parse([]byte("2021-05-26T12:37:00Z bar baz"))
	c.Check(err, tc.ErrorMatches, "invalid log service name")

	entry, err := servicelog.Parse([]byte("2021-05-26T12:37:00Z [bar] baz"))
	c.Assert(err, tc.ErrorIsNil)
	checkEntry(c, entry, servicelog.Entry{
		Time:    time.Date(2021, 5, 26, 12, 37, 0, 0, time.UTC),
		Service: "bar",
		Message: "baz",
	})

	entry, err = servicelog.Parse([]byte("2020-12-25T00:01:02.123456Z [x] a longer message\n"))
	c.Assert(err, tc.ErrorIsNil)
	checkEntry(c, entry, servicelog.Entry{
		Time:    time.Date(2020, 12, 25, 0, 1, 2, 123456000, time.UTC),
		Service: "x",
		Message: "a longer message\n",
	})
}

func checkEntry(c *tc.C, got, expected servicelog.Entry) {
	c.Check(got.Time.Equal(expected.Time), tc.Equals, true,
		tc.Commentf("expected timestamp %v, got %v", expected.Time, got.Time))
	c.Check(got.Service, tc.Equals, expected.Service)
	c.Check(got.Message, tc.Equals, expected.Message)
}

func (s *parserSuite) TestParser(c *tc.C) {
	// empty string
	parser := servicelog.NewParser(strings.NewReader(""), 1024)
	c.Check(parser.Next(), tc.Equals, false)
	c.Check(parser.Err(), tc.IsNil)

	// single invalid log line
	parser = servicelog.NewParser(strings.NewReader("foo"), 1024)
	c.Check(parser.Next(), tc.Equals, false)
	c.Check(parser.Err(), tc.IsNil)

	// invalid log line followed by valid
	parser = servicelog.NewParser(strings.NewReader("foo\n2021-05-26T12:37:00Z [s] msg\n"), 1024)
	c.Check(parser.Next(), tc.Equals, true)
	checkEntry(c, parser.Entry(), servicelog.Entry{
		Time:    time.Date(2021, 5, 26, 12, 37, 0, 0, time.UTC),
		Service: "s",
		Message: "msg\n",
	})
	c.Check(parser.Next(), tc.Equals, false)
	c.Check(parser.Err(), tc.IsNil)

	// valid log line followed by invalid (will use time/service of previous)
	parser = servicelog.NewParser(strings.NewReader(`
2021-05-26T12:37:00Z [s] msg
(... output truncated ...)
`), 1024)
	c.Check(parser.Next(), tc.Equals, true)
	checkEntry(c, parser.Entry(), servicelog.Entry{
		Time:    time.Date(2021, 5, 26, 12, 37, 0, 0, time.UTC),
		Service: "s",
		Message: "msg\n",
	})
	c.Check(parser.Next(), tc.Equals, true)
	checkEntry(c, parser.Entry(), servicelog.Entry{
		Time:    time.Date(2021, 5, 26, 12, 37, 0, 0, time.UTC),
		Service: "s",
		Message: "(... output truncated ...)\n",
	})
	c.Check(parser.Next(), tc.Equals, false)
	c.Check(parser.Err(), tc.IsNil)

	// too-small buffer
	parser = servicelog.NewParser(strings.NewReader(`
2021-05-26T12:37:00Z [s] msg
2021-05-26T12:37:01Z [s] a longish message
`), 30)
	c.Check(parser.Next(), tc.Equals, true)
	checkEntry(c, parser.Entry(), servicelog.Entry{
		Time:    time.Date(2021, 5, 26, 12, 37, 0, 0, time.UTC),
		Service: "s",
		Message: "msg\n",
	})
	c.Check(parser.Next(), tc.Equals, true)
	checkEntry(c, parser.Entry(), servicelog.Entry{
		Time:    time.Date(2021, 5, 26, 12, 37, 1, 0, time.UTC),
		Service: "s",
		Message: "a lon",
	})
	c.Check(parser.Next(), tc.Equals, true)
	checkEntry(c, parser.Entry(), servicelog.Entry{
		Time:    time.Date(2021, 5, 26, 12, 37, 1, 0, time.UTC),
		Service: "s",
		Message: "gish message\n",
	})
	c.Check(parser.Next(), tc.Equals, false)
	c.Check(parser.Err(), tc.IsNil)

	// Read error is handled
	parser = servicelog.NewParser(&errReader{errors.New("ERROR!")}, 1024)
	c.Check(parser.Next(), tc.Equals, false)
	c.Check(parser.Err(), tc.ErrorMatches, "ERROR!")
}

type errReader struct {
	err error
}

func (r *errReader) Read(p []byte) (int, error) {
	return 0, r.err
}

func (s *parserSuite) TestRoundTrip(c *tc.C) {
	start := time.Now().UTC()
	time.Sleep(10 * time.Millisecond) // to ensure that log timestamps are strictly after start
	buf := &bytes.Buffer{}
	fw := servicelog.NewFormatWriter(buf, "svc")
	for i := range 10 {
		fmt.Fprintf(fw, "message %d\n", i)
	}
	parser := servicelog.NewParser(buf, 1024)
	i := 0
	for parser.Next() {
		got := parser.Entry()
		c.Check(got.Time.After(start), tc.Equals, true,
			tc.Commentf("expected timestamp after %v, got %v", start, got.Time))
		c.Check(got.Service, tc.Equals, "svc")
		c.Check(got.Message, tc.Equals, fmt.Sprintf("message %d\n", i))
		i++
	}
	c.Check(parser.Err(), tc.IsNil)
}

func (s *parserSuite) TestNewDataAfterEOF(c *tc.C) {
	r := strings.NewReader("2021-05-26T12:37:00Z [s] msg1\n")
	parser := servicelog.NewParser(r, 1024)
	c.Check(parser.Next(), tc.Equals, true)
	checkEntry(c, parser.Entry(), servicelog.Entry{
		Time:    time.Date(2021, 5, 26, 12, 37, 0, 0, time.UTC),
		Service: "s",
		Message: "msg1\n",
	})
	c.Check(parser.Next(), tc.Equals, false)
	c.Check(parser.Err(), tc.IsNil)

	r.Reset("2021-05-26T12:37:01Z [s] msg2\n")
	c.Check(parser.Next(), tc.Equals, true)
	checkEntry(c, parser.Entry(), servicelog.Entry{
		Time:    time.Date(2021, 5, 26, 12, 37, 1, 0, time.UTC),
		Service: "s",
		Message: "msg2\n",
	})
	c.Check(parser.Next(), tc.Equals, false)
	c.Check(parser.Err(), tc.IsNil)
}
