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
	"errors"
	"io"
	"testing"

	"hegel.dev/go/hegel"

	"github.com/canonical/pebble/internals/servicelog"
)

// ringModel is a reference model of RingBuffer: the bytes that are still
// readable, and the absolute position of the first of them.
type ringModel struct {
	size  int
	start servicelog.RingPos
	data  []byte
}

func (m *ringModel) end() servicelog.RingPos {
	return m.start + servicelog.RingPos(len(m.data))
}

func (m *ringModel) write(p []byte) {
	m.data = append(m.data, p[:min(len(p), m.size)]...)
	m.discard(len(m.data) - m.size)
}

func (m *ringModel) discard(n int) {
	n = max(0, min(n, len(m.data)))
	m.start += servicelog.RingPos(n)
	m.data = m.data[n:]
}

// readPos resolves pos like RingBuffer does, reporting whether it is in range.
func (m *ringModel) readPos(pos servicelog.RingPos) (servicelog.RingPos, bool) {
	if pos == servicelog.TailPosition {
		pos = m.start
	}
	return pos, pos >= m.start && pos <= m.end()
}

func (m *ringModel) from(pos servicelog.RingPos) []byte {
	return m.data[pos-m.start:]
}

type ringBufferMachine struct {
	rb    *servicelog.RingBuffer
	model ringModel
}

// drawPos draws a position near (and occasionally outside) the readable range.
func (m *ringBufferMachine) drawPos(tc hegel.TestCase) servicelog.RingPos {
	if hegel.Draw(tc, hegel.Booleans()) {
		return servicelog.TailPosition
	}
	lo, hi := int64(m.model.start)-2, int64(m.model.end())+2
	return servicelog.RingPos(hegel.Draw(tc, hegel.Integers(lo, hi)))
}

func (m *ringBufferMachine) RuleWrite(tc hegel.TestCase) {
	p := hegel.Draw(tc, hegel.Binary(1, 2*m.model.size))
	n, err := m.rb.Write(p)
	if want := min(len(p), m.model.size); n != want {
		tc.Errorf("Write(%d bytes) wrote %d, want %d", len(p), n, want)
	}
	if len(p) > m.model.size && !errors.Is(err, io.ErrShortWrite) {
		tc.Errorf("Write(%d bytes) into %d byte buffer returned %v, want ErrShortWrite", len(p), m.model.size, err)
	}
	if len(p) <= m.model.size && err != nil {
		tc.Errorf("Write(%d bytes) returned %v", len(p), err)
	}
	m.model.write(p)
}

func (m *ringBufferMachine) RuleDiscard(tc hegel.TestCase) {
	n := hegel.Draw(tc, hegel.Integers(-1, m.model.size+1))
	err := m.rb.Discard(n)
	if n < 0 {
		if !errors.Is(err, servicelog.ErrRange) {
			tc.Errorf("Discard(%d) returned %v, want ErrRange", n, err)
		}
		return
	}
	if err != nil {
		tc.Errorf("Discard(%d) returned %v", n, err)
	}
	m.model.discard(n)
}

func (m *ringBufferMachine) RuleCopy(tc hegel.TestCase) {
	pos := m.drawPos(tc)
	dest := make([]byte, hegel.Draw(tc, hegel.Integers(0, m.model.size+1)))
	next, n, err := m.rb.Copy(dest, pos)

	readPos, ok := m.model.readPos(pos)
	switch {
	case !ok:
		if !errors.Is(err, servicelog.ErrRange) || n != 0 || next != pos {
			tc.Errorf("Copy(%d) outside [%d, %d] = (%d, %d, %v), want ErrRange", pos, m.model.start, m.model.end(), next, n, err)
		}
	case readPos == m.model.end():
		if !errors.Is(err, io.EOF) || n != 0 || next != pos {
			tc.Errorf("Copy(%d) at end = (%d, %d, %v), want EOF", pos, next, n, err)
		}
	case len(dest) == 0:
		if err != nil || n != 0 || next != pos {
			tc.Errorf("Copy(%d) into empty dest = (%d, %d, %v)", pos, next, n, err)
		}
	default:
		want := m.model.from(readPos)
		want = want[:min(len(want), len(dest))]
		wantNext := readPos + servicelog.RingPos(len(want))
		var wantErr error
		if wantNext == m.model.end() {
			wantErr = io.EOF
		}
		if !bytes.Equal(dest[:n], want) || next != wantNext || err != wantErr {
			tc.Errorf("Copy(%d) = (%d, %q, %v), want (%d, %q, %v)", pos, next, dest[:n], err, wantNext, want, wantErr)
		}
	}
}

func (m *ringBufferMachine) RuleWriteTo(tc hegel.TestCase) {
	pos := m.drawPos(tc)
	var buf bytes.Buffer
	next, n, err := m.rb.WriteTo(&buf, pos)

	readPos, ok := m.model.readPos(pos)
	switch {
	case !ok:
		if !errors.Is(err, servicelog.ErrRange) || n != 0 || next != pos {
			tc.Errorf("WriteTo(%d) outside [%d, %d] = (%d, %d, %v), want ErrRange", pos, m.model.start, m.model.end(), next, n, err)
		}
	case readPos == m.model.end():
		if err != nil || n != 0 || next != pos {
			tc.Errorf("WriteTo(%d) at end = (%d, %d, %v)", pos, next, n, err)
		}
	default:
		want := m.model.from(readPos)
		if err != nil || !bytes.Equal(buf.Bytes(), want) || n != int64(len(want)) || next != m.model.end() {
			tc.Errorf("WriteTo(%d) = (%d, %q, %v), want (%d, %q, nil)", pos, next, buf.Bytes(), err, m.model.end(), want)
		}
	}
}

func (m *ringBufferMachine) InvariantMatchesModel(tc hegel.TestCase) {
	start, end := m.rb.Positions()
	if start != m.model.start || end != m.model.end() {
		tc.Errorf("Positions() = (%d, %d), want (%d, %d)", start, end, m.model.start, m.model.end())
	}
	if buffered, available := m.rb.Buffered(), m.rb.Available(); buffered != len(m.model.data) || buffered+available != m.model.size {
		tc.Errorf("Buffered() = %d, Available() = %d, want %d and %d", buffered, available, len(m.model.data), m.model.size-len(m.model.data))
	}
	var buf bytes.Buffer
	if _, _, err := m.rb.WriteTo(&buf, servicelog.TailPosition); err != nil || !bytes.Equal(buf.Bytes(), m.model.data) {
		tc.Errorf("contents = %q (err %v), want %q", buf.Bytes(), err, m.model.data)
	}
}

func TestPropRingBufferMatchesModel(t *testing.T) {
	hegel.Test(t, func(ht *hegel.T) {
		size := hegel.Draw(ht, hegel.Integers(1, 32))
		hegel.RunStateful(ht, &ringBufferMachine{
			rb:    servicelog.NewRingBuffer(size),
			model: ringModel{size: size},
		})
	})
}
