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

	"github.com/canonical/pebble/internal/servicelog"
)

func BenchmarkWriteBufferConcurrentSmall(b *testing.B) {
	payload := []byte("p")
	benchmarkConcurrent(b, payload)
}

func BenchmarkWriteBufferConcurrent(b *testing.B) {
	payload := []byte("pebblepebblepebblepebble")
	benchmarkConcurrent(b, payload)
}

func benchmarkConcurrent(b *testing.B, payload []byte) {
	done := make(chan struct{})
	defer close(done)
	wb := servicelog.NewWriteBuffer(b.N, b.N*len(payload))
	go func() {
		for i := 0; i < b.N; i++ {
			select {
			case <-done:
				return
			default:
			}
			wb.Write(payload, servicelog.Stdout)
		}
	}()
	b.RunParallel(func(pb *testing.PB) {
		buf := make([]byte, len(payload))
		iterator := wb.TailIterator()
		for pb.Next() {
			more := iterator.More()
			for {
				if iterator.Next() {
					break
				}
				select {
				case <-more:
				case <-done:
					b.Fail()
					return
				}
			}
			iterator.Read(buf)
		}
	})
}
