// Copyright (c) 2014-2020 Canonical Ltd
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

package logger_test

import (
	"bytes"
	"fmt"
	"os"
	"testing"

	. "gopkg.in/check.v1"
	"gopkg.in/tomb.v2"

	"github.com/canonical/pebble/internals/logger"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

var _ = Suite(&LogSuite{})

type LogSuite struct {
	logbuf        fmt.Stringer
	restoreLogger func()
}

func (s *LogSuite) SetUpTest(c *C) {
	s.logbuf, s.restoreLogger = logger.MockLogger("PREFIX: ")
}

func (s *LogSuite) TearDownTest(c *C) {
	s.restoreLogger()
}

func (s *LogSuite) TestNew(c *C) {
	var buf bytes.Buffer
	l := logger.New(&buf, "")
	c.Assert(l, NotNil)
}

func (s *LogSuite) TestDebugf(c *C) {
	logger.Debugf("xyzzy")
	c.Check(s.logbuf.String(), Equals, "")
}

func (s *LogSuite) TestDebugfEnv(c *C) {
	os.Setenv("PEBBLE_DEBUG", "1")
	defer os.Unsetenv("PEBBLE_DEBUG")

	logger.Debugf("xyzzy")
	c.Check(s.logbuf.String(), Matches, `.* PREFIX: DEBUG xyzzy.*\n`)
}

func (s *LogSuite) TestNoticef(c *C) {
	logger.Noticef("xyzzy")
	c.Check(s.logbuf.String(), Matches, `20\d\d-\d\d-\d\dT\d\d:\d\d:\d\d.\d\d\dZ PREFIX: xyzzy\n`)
}

func (s *LogSuite) TestNewline(c *C) {
	logger.Noticef("with newline\n")
	c.Check(s.logbuf.String(), Matches, `20\d\d-\d\d-\d\dT\d\d:\d\d:\d\d.\d\d\dZ PREFIX: with newline\n`)
}

func (s *LogSuite) TestPanicf(c *C) {
	c.Check(func() { logger.Panicf("xyzzy") }, Panics, "xyzzy")
	c.Check(s.logbuf.String(), Matches, `20\d\d-\d\d-\d\dT\d\d:\d\d:\d\d.\d\d\dZ PREFIX: PANIC xyzzy\n`)
}

func (s *LogSuite) TestMockLoggerReadWriteThreadsafe(c *C) {
	var t tomb.Tomb
	t.Go(func() error {
		for i := 0; i < 100; i++ {
			logger.Noticef("foo")
			logger.Noticef("bar")
		}
		return nil
	})
	for i := 0; i < 10; i++ {
		logger.Noticef(s.logbuf.String())
	}
	err := t.Wait()
	c.Check(err, IsNil)
}
