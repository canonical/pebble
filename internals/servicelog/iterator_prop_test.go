// Copyright (c) 2026 Canonical Ltd
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

package servicelog_test

import (
	"bytes"
	"io"
	"testing"

	"hegel.dev/go/hegel"

	"github.com/canonical/pebble/internals/servicelog"
)

// truncatedMarker is what an iterator outputs when it has fallen behind the
// ring buffer and data has been lost. Generated output never contains "(",
// so the marker can't be confused with data.
var truncatedMarker = []byte("\n(... output truncated ...)\n")

var outputBytes = hegel.Map(hegel.Text().Alphabet("ab \n").MinSize(1).MaxSize(24), func(s string) []byte {
	return []byte(s)
})

type tailIteratorMachine struct {
	rb     *servicelog.RingBuffer
	it     servicelog.Iterator
	size   int
	stream []byte // everything accepted by the ring buffer
	out    bytes.Buffer
}

func (m *tailIteratorMachine) RuleWrite(tc hegel.TestCase) {
	p := hegel.Draw(tc, outputBytes)
	n, _ := m.rb.Write(p)
	m.stream = append(m.stream, p[:n]...)
}

func (m *tailIteratorMachine) RuleDiscard(tc hegel.TestCase) {
	m.rb.Discard(hegel.Draw(tc, hegel.Integers(0, m.size)))
}

func (m *tailIteratorMachine) RuleRead(tc hegel.TestCase) {
	dest := make([]byte, hegel.Draw(tc, hegel.Integers(1, m.size+1)))
	n, _ := m.it.Read(dest)
	m.out.Write(dest[:n])
}

func (m *tailIteratorMachine) RuleWriteTo(tc hegel.TestCase) {
	m.it.WriteTo(&m.out)
}

func (m *tailIteratorMachine) InvariantBuffered(tc hegel.TestCase) {
	if buffered := m.it.Buffered(); buffered < 0 || buffered > m.size {
		tc.Errorf("iterator Buffered() = %d, want within [0, %d]", buffered, m.size)
	}
}

// An iterator reading from the tail must output everything written, in
// order, except where it explicitly reports that output was truncated.
func TestPropTailIteratorOutputsStream(t *testing.T) {
	hegel.Test(t, func(ht *hegel.T) {
		size := hegel.Draw(ht, hegel.Integers(1, 32))
		rb := servicelog.NewRingBuffer(size)
		m := &tailIteratorMachine{rb: rb, it: rb.TailIterator(), size: size}
		defer m.it.Close()
		hegel.RunStateful(ht, m)

		for i := 0; m.it.Next(nil); i++ {
			if i > 10 {
				ht.Fatalf("Next keeps reporting more to read")
			}
			if _, err := m.out.ReadFrom(m.it); err != nil {
				ht.Fatalf("ReadFrom: %v", err)
			}
		}

		segments := bytes.Split(m.out.Bytes(), truncatedMarker)
		if !bytes.HasPrefix(m.stream, segments[0]) {
			ht.Fatalf("output %q does not start at the beginning of %q", m.out.Bytes(), m.stream)
		}
		if !bytes.HasSuffix(m.stream, segments[len(segments)-1]) {
			ht.Fatalf("output %q does not finish at the end of %q", m.out.Bytes(), m.stream)
		}
		pos := len(segments[0])
		for _, segment := range segments[1:] {
			i := bytes.Index(m.stream[pos:], segment)
			if i < 0 {
				ht.Fatalf("output %q is not an ordered selection of %q", m.out.Bytes(), m.stream)
			}
			pos += i + len(segment)
		}
	})
}

// HeadIterator(n) must start at the beginning of a line, and output the last
// n complete lines (plus any incomplete line after them), or all of them if
// there are fewer than n.
func TestPropHeadIteratorLastLines(t *testing.T) {
	hegel.Test(t, func(ht *hegel.T) {
		rb := servicelog.NewRingBuffer(hegel.Draw(ht, hegel.Integers(1, 64)))
		for _, p := range hegel.Draw(ht, hegel.Lists(outputBytes).MaxSize(8)) {
			rb.Write(p)
		}
		n := hegel.Draw(ht, hegel.Integers(-1, 10))

		var contents bytes.Buffer
		rb.WriteTo(&contents, servicelog.TailPosition)
		it := rb.HeadIterator(n)
		defer it.Close()
		out, err := io.ReadAll(it)
		if err != nil {
			ht.Fatalf("ReadAll: %v", err)
		}

		if n <= 0 {
			if len(out) != 0 {
				ht.Fatalf("HeadIterator(%d) of %q output %q, want nothing", n, contents.Bytes(), out)
			}
			return
		}
		before, ok := bytes.CutSuffix(contents.Bytes(), out)
		if !ok {
			ht.Fatalf("HeadIterator(%d) of %q output %q, which is not a suffix", n, contents.Bytes(), out)
		}
		if len(before) > 0 && before[len(before)-1] != '\n' {
			ht.Fatalf("HeadIterator(%d) of %q output %q, which starts mid-line", n, contents.Bytes(), out)
		}
		// A leading empty line is skipped when there are fewer than n lines.
		lines := bytes.Count(out, []byte("\n"))
		if lines > n || (lines < n && len(before) > 0 && !bytes.Equal(before, []byte("\n"))) {
			ht.Fatalf("HeadIterator(%d) of %q output %q, which has %d lines", n, contents.Bytes(), out, lines)
		}
	})
}
