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

package testutil

import (
	"bytes"
	"runtime"
	"runtime/pprof"
	"testing"
)

func PrintGoroutineLeaks(t *testing.T, f func(t *testing.T)) {
	t.Helper()

	grl := pprof.Lookup("goroutineleak")
	if grl == nil {
		f(t)
		return
	}

	done := make(chan struct{})
	go leakSentinal(done)
	defer func() {
		// leak the sentinal
		done = nil

		// find the sentinal in the go routine leak profile
		out := &bytes.Buffer{}
		sentinalBytes := []byte("leakSentinal")
		for {
			_ = grl.WriteTo(out, 2)
			if bytes.Contains(out.Bytes(), sentinalBytes) {
				break
			}
			out.Reset()
			runtime.GC()
		}

		// find any leaked go routines other than the sentinal
		leakedBytes := []byte("(leaked)")
		leaked := false
		for stack := range bytes.SplitSeq(out.Bytes(), []byte("\n\n")) {
			if bytes.Contains(stack, sentinalBytes) ||
				!bytes.Contains(stack, leakedBytes) {
				continue
			}
			if leaked {
				_, _ = t.Output().Write([]byte("\n\n"))
			}
			_, _ = t.Output().Write(stack)
			leaked = true
		}

		// mark the test as failed if any leaked go routines was found
		if leaked {
			t.Fail()
		}
	}()

	f(t)
}

func leakSentinal(done chan struct{}) {
	<-done
}
