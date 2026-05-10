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

package progress_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/canonical/tc"

	"github.com/canonical/pebble/internals/progress"
)

type ansiSuite struct{}

func TestAnsiSuite(t *testing.T) {
	tc.Run(t, &ansiSuite{})
}

func (ansiSuite) TestNorm(c *tc.C) {
	msg := []rune(strings.Repeat("0123456789", 100))
	high := []rune("🤗🤗🤗🤗🤗")
	c.Assert(msg, tc.HasLen, 1000)
	for i := 1; i < 1000; i += 1 {
		long := progress.Norm(i, msg)
		short := progress.Norm(i, nil)
		// a long message is truncated to fit
		c.Check(long, tc.HasLen, i)
		c.Check(long[len(long)-1], tc.Equals, rune('…'))
		// a short message is padded to width
		c.Check(short, tc.HasLen, i)
		c.Check(string(short), tc.Equals, strings.Repeat(" ", i))
		// high unicode? no problem
		c.Check(progress.Norm(i, high), tc.HasLen, i)
	}
	// check it doesn't panic for negative nor zero widths
	c.Check(progress.Norm(0, []rune("hello")), tc.HasLen, 0)
	c.Check(progress.Norm(-10, []rune("hello")), tc.HasLen, 0)
}

func (ansiSuite) TestPercent(c *tc.C) {
	p := &progress.ANSIMeter{}
	for i := -1000.; i < 1000.; i += 5 {
		p.SetTotal(i)
		for j := -1000.; j < 1000.; j += 3 {
			p.SetWritten(j)
			percent := p.Percent()
			c.Check(percent, tc.HasLen, 4)
			c.Check(percent[len(percent)-1:], tc.Equals, "%")
		}
	}
}

func (ansiSuite) TestStart(c *tc.C) {
	var buf bytes.Buffer
	defer progress.MockStdout(&buf)()
	defer progress.MockTermWidth(func() int { return 80 })()

	p := &progress.ANSIMeter{}
	p.Start("0123456789", 100)
	c.Check(p.GetTotal(), tc.Equals, 100.)
	c.Check(p.GetWritten(), tc.Equals, 0.)
	c.Check(buf.String(), tc.Equals, progress.CursorInvisible)
}

func (ansiSuite) TestFinish(c *tc.C) {
	var buf bytes.Buffer
	defer progress.MockStdout(&buf)()
	defer progress.MockTermWidth(func() int { return 80 })()
	p := &progress.ANSIMeter{}
	p.Finished()
	c.Check(buf.String(), tc.Equals, fmt.Sprint(
		"\r",                       // move cursor to start of line
		progress.ExitAttributeMode, // turn off color, reverse, bold, anything
		progress.CursorVisible,     // turn the cursor back on
		progress.ClrEOL,            // and clear the rest of the line
	))
}

func (ansiSuite) TestSetLayout(c *tc.C) {
	var buf bytes.Buffer
	var width int
	defer progress.MockStdout(&buf)()
	defer progress.MockEmptyEscapes()()
	defer progress.MockTermWidth(func() int { return width })()

	p := &progress.ANSIMeter{}
	msg := "0123456789"
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	p.Start(msg, 1e300)
	for i := 1; i <= 80; i++ {
		desc := tc.Commentf("width %d", i)
		width = i
		buf.Reset()
		<-ticker.C
		p.Set(float64(i))
		out := buf.String()
		c.Check([]rune(out), tc.HasLen, i+1, desc)
		switch {
		case i < len(msg):
			c.Check(out, tc.Equals, "\r"+msg[:i-1]+"…", desc)
		case i <= 15:
			c.Check(out, tc.Equals, fmt.Sprintf("\r%*s", -i, msg), desc)
		case i <= 20:
			c.Check(out, tc.Equals, fmt.Sprintf("\r%*s ages!", -(i-6), msg), desc)
		case i <= 29:
			c.Check(out, tc.Equals, fmt.Sprintf("\r%*s   0%% ages!", -(i-11), msg), desc)
		default:
			c.Check(out, tc.Matches, fmt.Sprintf("\r%*s   0%%  [ 0-9]{4}B/s ages!", -(i-20), msg), desc)
		}
	}
}

func (ansiSuite) TestSetEscapes(c *tc.C) {
	var buf bytes.Buffer
	defer progress.MockStdout(&buf)()
	defer progress.MockSimpleEscapes()()
	defer progress.MockTermWidth(func() int { return 10 })()

	p := &progress.ANSIMeter{}
	msg := "0123456789"
	p.Start(msg, 10)
	for i := 0.; i <= 10; i++ {
		buf.Reset()
		p.Set(i)
		// here we're using the fact that the message has the same
		// length as p's total to make the test simpler :-)
		expected := "\r<MR>" + msg[:int(i)] + "<ME>" + msg[int(i):]
		c.Check(buf.String(), tc.Equals, expected, tc.Commentf("%g", i))
	}
}

func (ansiSuite) TestSpin(c *tc.C) {
	termWidth := 9
	var buf bytes.Buffer
	defer progress.MockStdout(&buf)()
	defer progress.MockSimpleEscapes()()
	defer progress.MockTermWidth(func() int { return termWidth })()

	p := &progress.ANSIMeter{}
	msg := "0123456789"
	c.Assert(len(msg), tc.Equals, 10)
	p.Start(msg, 10)

	// term too narrow to fit msg
	for i, s := range progress.Spinner {
		buf.Reset()
		p.Spin(msg)
		expected := "\r" + msg[:8] + "…"
		c.Check(buf.String(), tc.Equals, expected, tc.Commentf("%d (%s)", i, s))
	}

	// term fits msg but not spinner
	termWidth = 11
	for i, s := range progress.Spinner {
		buf.Reset()
		p.Spin(msg)
		expected := "\r" + msg + " "
		c.Check(buf.String(), tc.Equals, expected, tc.Commentf("%d (%s)", i, s))
	}

	// term fits msg and spinner
	termWidth = 12
	for i, s := range progress.Spinner {
		buf.Reset()
		p.Spin(msg)
		expected := "\r" + msg + " " + s
		c.Check(buf.String(), tc.Equals, expected, tc.Commentf("%d (%s)", i, s))
	}
}

func (ansiSuite) TestNotify(c *tc.C) {
	var buf bytes.Buffer
	var width int
	defer progress.MockStdout(&buf)()
	defer progress.MockSimpleEscapes()()
	defer progress.MockTermWidth(func() int { return width })()

	p := &progress.ANSIMeter{}
	p.Start("working", 1e300)

	width = 10
	p.Set(0)
	p.Notify("hello there")
	p.Set(1)
	c.Check(buf.String(), tc.Equals, "<VI>"+ // the VI from Start()
		"\r<MR><ME>working   "+ // the Set(0)
		"\r<ME><CE>hello\n"+ // first line of the Notify (note it wrapped at word end)
		"there\n"+
		"\r<MR><ME>working   ") // the Set(1)

	buf.Reset()
	p.Set(0)
	p.Notify("supercalifragilisticexpialidocious")
	p.Set(1)
	c.Check(buf.String(), tc.Equals, ""+ // no Start() this time
		"\r<MR><ME>working   "+ // the Set(0)
		"\r<ME><CE>supercalif\n"+ // the Notify, word is too long so it's just split
		"ragilistic\n"+
		"expialidoc\n"+
		"ious\n"+
		"\r<MR><ME>working   ") // the Set(1)

	buf.Reset()
	width = 16
	p.Set(0)
	p.Notify("hello there")
	p.Set(1)
	c.Check(buf.String(), tc.Equals, ""+ // no Start()
		"\r<MR><ME>working    ages!"+ // the Set(0)
		"\r<ME><CE>hello there\n"+ // first line of the Notify (no wrap!)
		"\r<MR><ME>working    ages!") // the Set(1)

}

func (ansiSuite) TestWrite(c *tc.C) {
	var buf bytes.Buffer
	defer progress.MockStdout(&buf)()
	defer progress.MockSimpleEscapes()()
	defer progress.MockTermWidth(func() int { return 10 })()

	p := &progress.ANSIMeter{}
	p.Start("123456789x", 10)
	for i := range 10 {
		n, err := fmt.Fprintf(p, "%d", i)
		c.Assert(err, tc.IsNil)
		c.Check(n, tc.Equals, 1)
	}

	c.Check(buf.String(), tc.Equals, strings.Join([]string{
		"<VI>", // Start()
		"\r<MR>1<ME>23456789x",
		"\r<MR>12<ME>3456789x",
		"\r<MR>123<ME>456789x",
		"\r<MR>1234<ME>56789x",
		"\r<MR>12345<ME>6789x",
		"\r<MR>123456<ME>789x",
		"\r<MR>1234567<ME>89x",
		"\r<MR>12345678<ME>9x",
		"\r<MR>123456789<ME>x",
		"\r<MR>123456789x<ME>",
	}, ""))
}
