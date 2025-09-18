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
	"io"
	"testing"
)

// BenchmarkLoggerNoticef tests Noticef with string formatting.
func BenchmarkLoggerNoticef(b *testing.B) {
	logger := New(io.Discard, "Benchmark: ")
	SetLogger(logger)

	b.ResetTimer()
	for b.Loop() {
		Noticef("Formatted message with number: %d and string: %s", 42, "test")
	}
}

// BenchmarkLoggerNoticefConcurrent tests concurrent logging performance.
func BenchmarkLoggerNoticefConcurrent(b *testing.B) {
	logger := New(io.Discard, "Benchmark: ")
	SetLogger(logger)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			Noticef("Concurrent message from goroutiner: %d and string: %s", 42, "test")
		}
	})
}
