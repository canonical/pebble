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
	p1 := rb.Pos()
	_, err := rb.Write(payload)
	if err != nil {
		b.Fatal(err)
	}
	p2 := rb.Pos()

	buffer := make([]byte, len(payload))
	for i := 0; i < b.N; i++ {
		rb.Copy(buffer, p1, p2)
	}
}
