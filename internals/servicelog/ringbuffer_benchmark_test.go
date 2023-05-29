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
	"testing"

	"github.com/canonical/pebble/internals/servicelog"
)

func BenchmarkRingBufferWriteSmall(b *testing.B) {
	payload := []byte("p")
	rb := servicelog.NewRingBuffer(len(payload) * b.N)
	for i := 0; i < b.N; i++ {
		rb.Write(payload)
	}
}

func BenchmarkRingBufferWrite(b *testing.B) {
	payload := []byte("pebblepebblepebblepebble")
	rb := servicelog.NewRingBuffer(len(payload) * b.N)
	for i := 0; i < b.N; i++ {
		rb.Write(payload)
	}
}

func BenchmarkRingBufferCopy(b *testing.B) {
	payload := []byte("pebblepebblepebblepebble")
	rb := servicelog.NewRingBuffer(len(payload))
	_, err := rb.Write(payload)
	if err != nil {
		b.Fatal(err)
	}
	p1, _ := rb.Positions()

	buffer := make([]byte, len(payload))
	for i := 0; i < b.N; i++ {
		rb.Copy(buffer, p1)
	}
}

func BenchmarkRingBufferConcurrentSmall(b *testing.B) {
	payload := []byte("p")
	benchmarkConcurrent(b, payload)
}

func BenchmarkRingBufferConcurrent(b *testing.B) {
	payload := []byte("pebblepebblepebblepebble")
	benchmarkConcurrent(b, payload)
}

func benchmarkConcurrent(b *testing.B, payload []byte) {
	n := b.N
	wb := servicelog.NewRingBuffer(n * len(payload))
	defer wb.Close()
	done := make(chan struct{})
	defer close(done)
	go func() {
		for i := 0; i < n; i++ {
			wb.Write(payload)
		}
	}()
	b.RunParallel(func(pb *testing.PB) {
		iterator := wb.TailIterator()
		defer iterator.Close()
		buf := make([]byte, len(payload))
		for pb.Next() {
			if iterator.Next(done) {
				iterator.Read(buf)
			}
		}
	})
}
