// Copyright (c) 2025 Canonical Ltd
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

package logger

import (
	"testing"
	"time"
)

func BenchmarkAppendFormat(b *testing.B) {
	t := time.Now()
	buf := make([]byte, 0, 64)

	for b.Loop() {
		buf = t.UTC().AppendFormat(buf[:0], "2006-01-02T15:04:05.000Z")
	}

	_ = buf // ensure buf is not optimized away
}

func BenchmarkAppendTimestamp(b *testing.B) {
	t := time.Now()
	buf := make([]byte, 0, 64)

	for b.Loop() {
		buf = AppendTimestamp(buf[:0], t)
	}

	_ = buf // ensure buf is not optimized away
}
