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
	"time"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internal/servicelog"
)

type parserSuite struct{}

var _ = Suite(&parserSuite{})

func (s *parserSuite) TestParse(c *C) {
	_, err := servicelog.Parse([]byte("foo bar"))
	c.Check(err, ErrorMatches, "line has too few fields")

	_, err = servicelog.Parse([]byte("foo bar baz"))
	c.Check(err, ErrorMatches, "invalid log timestamp")

	_, err = servicelog.Parse([]byte("2021-05-26T12:37:00Z [] baz"))
	c.Check(err, ErrorMatches, "invalid log service name")

	_, err = servicelog.Parse([]byte("2021-05-26T12:37:00Z bar baz"))
	c.Check(err, ErrorMatches, "invalid log service name")

	entry, err := servicelog.Parse([]byte("2021-05-26T12:37:00Z [bar] baz"))
	c.Check(err, IsNil)
	checkEntry(c, entry, servicelog.Entry{
		Time:    time.Date(2021, 5, 26, 12, 37, 0, 0, time.UTC),
		Service: "bar",
		Message: "baz",
	})

	entry, err = servicelog.Parse([]byte("2020-12-25T00:01:02.123456Z [x] a longer message\n"))
	c.Check(err, IsNil)
	checkEntry(c, entry, servicelog.Entry{
		Time:    time.Date(2020, 12, 25, 0, 1, 2, 123456000, time.UTC),
		Service: "x",
		Message: "a longer message\n",
	})
}

func checkEntry(c *C, got, expected servicelog.Entry) {
	c.Check(got.Time.Equal(expected.Time), Equals, true,
		Commentf("expected timestamp %v, got %v", expected.Time, got.Time))
	c.Check(got.Service, Equals, expected.Service)
	c.Check(got.Message, Equals, expected.Message)
}

func (s *parserSuite) TestParser(c *C) {
	// empty string
	parser := servicelog.NewParser(strings.NewReader(""), 1024)
	c.Check(parser.Next(), Equals, false)
	c.Check(parser.Err(), IsNil)

	// single invalid log line
	parser = servicelog.NewParser(strings.NewReader("foo"), 1024)
	c.Check(parser.Next(), Equals, false)
	c.Check(parser.Err(), IsNil)

	// invalid log line followed by valid
	parser = servicelog.NewParser(strings.NewReader("foo\n2021-05-26T12:37:00Z [s] msg\n"), 1024)
	c.Check(parser.Next(), Equals, true)
	checkEntry(c, parser.Entry(), servicelog.Entry{
		Time:    time.Date(2021, 5, 26, 12, 37, 0, 0, time.UTC),
		Service: "s",
		Message: "msg\n",
	})
	c.Check(parser.Next(), Equals, false)
	c.Check(parser.Err(), IsNil)

	// valid log line followed by invalid (will use time/service of previous)
	parser = servicelog.NewParser(strings.NewReader(`
2021-05-26T12:37:00Z [s] msg
(... output truncated ...)
`), 1024)
	c.Check(parser.Next(), Equals, true)
	checkEntry(c, parser.Entry(), servicelog.Entry{
		Time:    time.Date(2021, 5, 26, 12, 37, 0, 0, time.UTC),
		Service: "s",
		Message: "msg\n",
	})
	c.Check(parser.Next(), Equals, true)
	checkEntry(c, parser.Entry(), servicelog.Entry{
		Time:    time.Date(2021, 5, 26, 12, 37, 0, 0, time.UTC),
		Service: "s",
		Message: "(... output truncated ...)\n",
	})
	c.Check(parser.Next(), Equals, false)
	c.Check(parser.Err(), IsNil)

	// too-small buffer
	parser = servicelog.NewParser(strings.NewReader(`
2021-05-26T12:37:00Z [s] msg
2021-05-26T12:37:01Z [s] a longish message
`), 30)
	c.Check(parser.Next(), Equals, true)
	checkEntry(c, parser.Entry(), servicelog.Entry{
		Time:    time.Date(2021, 5, 26, 12, 37, 0, 0, time.UTC),
		Service: "s",
		Message: "msg\n",
	})
	c.Check(parser.Next(), Equals, true)
	checkEntry(c, parser.Entry(), servicelog.Entry{
		Time:    time.Date(2021, 5, 26, 12, 37, 1, 0, time.UTC),
		Service: "s",
		Message: "a lon",
	})
	c.Check(parser.Next(), Equals, true)
	checkEntry(c, parser.Entry(), servicelog.Entry{
		Time:    time.Date(2021, 5, 26, 12, 37, 1, 0, time.UTC),
		Service: "s",
		Message: "gish message\n",
	})
	c.Check(parser.Next(), Equals, false)
	c.Check(parser.Err(), IsNil)

	// Read error is handled
	parser = servicelog.NewParser(&errReader{errors.New("ERROR!")}, 1024)
	c.Check(parser.Next(), Equals, false)
	c.Check(parser.Err(), ErrorMatches, "ERROR!")
}

type errReader struct {
	err error
}

func (r *errReader) Read(p []byte) (int, error) {
	return 0, r.err
}

func (s *parserSuite) TestRoundTrip(c *C) {
	start := time.Now().UTC()
	time.Sleep(10 * time.Millisecond) // to ensure that log timestamps are strictly after start
	buf := &bytes.Buffer{}
	fw := servicelog.NewFormatWriter(buf, "svc")
	for i := 0; i < 10; i++ {
		fmt.Fprintf(fw, "message %d\n", i)
	}
	parser := servicelog.NewParser(buf, 1024)
	i := 0
	for parser.Next() {
		got := parser.Entry()
		c.Check(got.Time.After(start), Equals, true,
			Commentf("expected timestamp after %v, got %v", start, got.Time))
		c.Check(got.Service, Equals, "svc")
		c.Check(got.Message, Equals, fmt.Sprintf("message %d\n", i))
		i++
	}
	c.Check(parser.Err(), IsNil)
}

func (s *parserSuite) TestNewDataAfterEOF(c *C) {
	r := strings.NewReader("2021-05-26T12:37:00Z [s] msg1\n")
	parser := servicelog.NewParser(r, 1024)
	c.Check(parser.Next(), Equals, true)
	checkEntry(c, parser.Entry(), servicelog.Entry{
		Time:    time.Date(2021, 5, 26, 12, 37, 0, 0, time.UTC),
		Service: "s",
		Message: "msg1\n",
	})
	c.Check(parser.Next(), Equals, false)
	c.Check(parser.Err(), IsNil)

	r.Reset("2021-05-26T12:37:01Z [s] msg2\n")
	c.Check(parser.Next(), Equals, true)
	checkEntry(c, parser.Entry(), servicelog.Entry{
		Time:    time.Date(2021, 5, 26, 12, 37, 1, 0, time.UTC),
		Service: "s",
		Message: "msg2\n",
	})
	c.Check(parser.Next(), Equals, false)
	c.Check(parser.Err(), IsNil)
}
