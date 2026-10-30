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
	"strings"
	"testing"

	"hegel.dev/go/hegel"

	"github.com/canonical/pebble/internals/servicelog"
)

// ServiceLastLines must return exactly the last n lines the service wrote,
// without their log prefixes, marking any earlier lines with "(...)".
func TestPropServiceLastLines(t *testing.T) {
	hegel.Test(t, func(ht *hegel.T) {
		serviceName := hegel.Draw(ht, hegel.Text().Alphabet("ab]").MinSize(1).MaxSize(4))
		messages := hegel.Draw(ht, hegel.Lists(hegel.Text().ExcludeCharacters("\n").MaxSize(10)).MaxSize(6))
		n := hegel.Draw(ht, hegel.Integers(1, 8))
		indent := hegel.Draw(ht, hegel.SampledFrom([]string{"", "    "}))

		rb := servicelog.NewRingBuffer(1 << 16)
		fw := servicelog.NewFormatWriter(rb, serviceName)
		for _, message := range messages {
			if _, err := fw.Write([]byte(message + "\n")); err != nil {
				ht.Fatalf("Write: %v", err)
			}
		}

		want := messages
		if len(messages) > n {
			want = append([]string{"(...)"}, messages[len(messages)-n:]...)
		}
		for i := range want {
			want[i] = indent + want[i]
		}
		got, err := servicelog.ServiceLastLines(rb, n, indent, serviceName)
		if err != nil {
			ht.Fatalf("ServiceLastLines: %v", err)
		}
		if wantStr := strings.Join(want, "\n"); got != wantStr {
			ht.Fatalf("ServiceLastLines(%d) = %q, want %q", n, got, wantStr)
		}

		// LastLines can only find the end of the prefix if the service name
		// doesn't contain "]".
		if strings.Contains(serviceName, "]") {
			return
		}
		got, err = servicelog.LastLines(rb, n, indent, true)
		if err != nil {
			ht.Fatalf("LastLines: %v", err)
		}
		if wantStr := strings.Join(want, "\n"); got != wantStr {
			ht.Fatalf("LastLines(%d) = %q, want %q", n, got, wantStr)
		}
	})
}
