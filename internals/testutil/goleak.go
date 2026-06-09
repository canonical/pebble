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

	leakProfile := pprof.Lookup("goroutineleak")
	if leakProfile == nil {
		f(t)
		return
	}

	done := make(chan struct{})
	go leakSentinel(done)
	defer func() {
		// Leak the sentinel.
		done = nil

		// Find the sentinal in the goroutine leak profile.
		out := &bytes.Buffer{}
		sentinelBytes := []byte("leakSentinel")
		for {
			_ = leakProfile.WriteTo(out, 2)
			// Break out of the loop if the leaked sentinel was discovered in
			// the leak profile. Otherwise continue until the test harness times
			// out.
			if bytes.Contains(out.Bytes(), sentinelBytes) {
				break
			}
			out.Reset()
			runtime.GC()
		}

		// Find any leaked goroutines other than the sentinel.
		leakedBytes := []byte("(leaked)")
		leaked := false
		for stack := range bytes.SplitSeq(out.Bytes(), []byte("\n\n")) {
			isSentinel := bytes.Contains(stack, sentinelBytes)
			isLeak := bytes.Contains(stack, leakedBytes)
			if isSentinel || !isLeak {
				// Ignore both the sentinel leak and non-leaked goroutines.
				continue
			}
			if leaked {
				_, _ = t.Output().Write([]byte("\n\n"))
			}
			_, _ = t.Output().Write(stack)
			leaked = true
		}

		// Mark the test as failed if any leaked goroutines were found.
		if leaked {
			t.Fail()
		}
	}()

	f(t)
}

func leakSentinel(done chan struct{}) {
	<-done
}
