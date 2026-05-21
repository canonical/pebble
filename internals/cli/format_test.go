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

package cli_test

import (
	"os"
	"runtime"

	"github.com/canonical/tc"

	"github.com/canonical/pebble/internals/cli"
)

func setEnviron(env map[string]string) func() {
	old := make(map[string]string, len(env))
	ok := make(map[string]bool, len(env))

	for k, v := range env {
		old[k], ok[k] = os.LookupEnv(k)
		if v != "" {
			os.Setenv(k, v)
		} else {
			os.Unsetenv(k)
		}
	}

	return func() {
		for k := range ok {
			if ok[k] {
				os.Setenv(k, old[k])
			} else {
				os.Unsetenv(k)
			}
		}
	}
}

func (s *PebbleSuite) TestCanUnicode(c *tc.C) {
	// setenv is per thread
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	type T struct {
		lang, lcAll, lcMsg string
		expected           bool
	}

	for _, t := range []T{
		{expected: false}, // all locale unset
		{lang: "C", expected: false},
		{lang: "C", lcAll: "C", expected: false},
		{lang: "C", lcAll: "C", lcMsg: "C", expected: false},
		{lang: "C.UTF-8", lcAll: "C", lcMsg: "C", expected: false}, // LC_MESSAGES wins
		{lang: "C.UTF-8", lcAll: "C.UTF-8", lcMsg: "C", expected: false},
		{lang: "C.UTF-8", lcAll: "C.UTF-8", lcMsg: "C.UTF-8", expected: true},
		{lang: "C.UTF-8", lcAll: "C", lcMsg: "C.UTF-8", expected: true},
		{lang: "C", lcAll: "C", lcMsg: "C.UTF-8", expected: true},
		{lang: "C", lcAll: "C.UTF-8", expected: true},
		{lang: "C.UTF-8", expected: true},
		{lang: "C.utf8", expected: true}, // deals with a bit of rando weirdness
	} {
		restore := setEnviron(map[string]string{"LANG": t.lang, "LC_ALL": t.lcAll, "LC_MESSAGES": t.lcMsg})
		c.Check(cli.CanUnicode("never"), tc.Equals, false)
		c.Check(cli.CanUnicode("always"), tc.Equals, true)
		restoreIsTTY := cli.FakeIsStdoutTTY(true)
		c.Check(cli.CanUnicode("auto"), tc.Equals, t.expected)
		cli.FakeIsStdoutTTY(false)
		c.Check(cli.CanUnicode("auto"), tc.Equals, false)
		restoreIsTTY()
		restore()
	}
}

func (s *PebbleSuite) TestColorTable(c *tc.C) {
	// setenv is per thread
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	type T struct {
		isTTY         bool
		noColor, term string
		expected      any
		desc          string
	}

	for _, t := range []T{
		{isTTY: false, expected: cli.NoEscColorTable, desc: "not a tty"},
		{isTTY: false, noColor: "1", expected: cli.NoEscColorTable, desc: "no tty *and* NO_COLOR set"},
		{isTTY: false, term: "linux-m", expected: cli.NoEscColorTable, desc: "no tty *and* mono term set"},
		{isTTY: true, expected: cli.ColorColorTable, desc: "is a tty"},
		{isTTY: true, noColor: "1", expected: cli.MonoColorTable, desc: "is a tty, but NO_COLOR set"},
		{isTTY: true, term: "linux-m", expected: cli.MonoColorTable, desc: "is a tty, but TERM=linux-m"},
		{isTTY: true, term: "xterm-mono", expected: cli.MonoColorTable, desc: "is a tty, but TERM=xterm-mono"},
	} {
		restoreIsTTY := cli.FakeIsStdoutTTY(t.isTTY)
		restoreEnv := setEnviron(map[string]string{"NO_COLOR": t.noColor, "TERM": t.term})
		c.Check(cli.ColorTable("never"), tc.DeepEquals, cli.NoEscColorTable, tc.Commentf(t.desc))
		c.Check(cli.ColorTable("always"), tc.DeepEquals, cli.ColorColorTable, tc.Commentf(t.desc))
		c.Check(cli.ColorTable("auto"), tc.DeepEquals, t.expected, tc.Commentf(t.desc))
		restoreEnv()
		restoreIsTTY()
	}
}
