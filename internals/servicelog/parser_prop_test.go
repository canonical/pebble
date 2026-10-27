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
	"time"
	"unicode"

	"hegel.dev/go/hegel"

	"github.com/canonical/pebble/internals/servicelog"
)

// logChunk generates pieces of service output, biased towards the bytes
// that are significant to the log format.
var logChunk = hegel.OneOf(
	hegel.Text().MaxSize(20),
	hegel.SampledFrom([]string{"\n", " ", "[", "]", "] ", "2021-05-26T12:37:00Z [x] y\n"}),
)

// Everything a service writes must come back out of the parser, attributed
// to that service, however the output is split into writes and whatever the
// size of the parser's buffer.
func TestPropFormatParseRoundTrip(t *testing.T) {
	hegel.Test(t, func(ht *hegel.T) {
		serviceName := hegel.Draw(ht, hegel.Text().MinSize(1).MaxSize(8))
		ht.Assume(serviceName != "pebble" && !strings.HasPrefix(serviceName, "-"))
		// Layer validation rejects service names containing whitespace.
		ht.Assume(!strings.ContainsFunc(serviceName, unicode.IsSpace))
		writes := hegel.Draw(ht, hegel.Lists(logChunk).MaxSize(10))
		// The parser can't recover a line whose "<timestamp> [<service>] "
		// prefix doesn't fit in its buffer.
		prefixLen := len("2006-01-02T15:04:05.000Z [] ") + len(serviceName)
		parserSize := hegel.Draw(ht, hegel.Integers(max(16, prefixLen), prefixLen+256))

		before := time.Now().Truncate(time.Millisecond)
		rb := servicelog.NewRingBuffer(1 << 20)
		it := rb.TailIterator()
		defer it.Close()
		fw := servicelog.NewFormatWriter(rb, serviceName)
		parser := servicelog.NewParser(it, parserSize)

		var got strings.Builder
		// Read the same way as logstate's puller: parser.Next returns false
		// whenever it runs out of buffered data, even mid-line.
		drain := func() {
			for it.Next(nil) {
				for parser.Next() {
					entry := parser.Entry()
					if entry.Service != serviceName {
						ht.Fatalf("entry %q has service %q, want %q", entry.Message, entry.Service, serviceName)
					}
					if entry.Time.Before(before) || entry.Time.After(time.Now()) {
						ht.Fatalf("entry %q has time %v, want after %v", entry.Message, entry.Time, before)
					}
					got.WriteString(entry.Message)
				}
			}
		}
		for _, w := range writes {
			if _, err := fw.Write([]byte(w)); err != nil {
				ht.Fatalf("Write: %v", err)
			}
			drain()
		}
		drain()

		if err := parser.Err(); err != nil {
			ht.Fatalf("parser error: %v", err)
		}
		if want := strings.Join(writes, ""); got.String() != want {
			ht.Fatalf("parsed messages %q, want %q", got.String(), want)
		}

		// Each line must have its own prefix, rather than relying on the
		// parser to treat it as a continuation.
		var formatted strings.Builder
		rb.WriteTo(&formatted, servicelog.TailPosition)
		for _, line := range strings.SplitAfter(formatted.String(), "\n") {
			if line == "" {
				continue
			}
			if entry, err := servicelog.Parse([]byte(line)); err != nil || entry.Service != serviceName {
				ht.Fatalf("formatted line %q does not have a %q prefix", line, serviceName)
			}
		}
	}, hegel.WithTestCases(1000))
}

// Parse must never panic, and a successful parse must account for the
// whole line.
func TestPropParseIsConsistent(t *testing.T) {
	hegel.Test(t, func(ht *hegel.T) {
		line := hegel.Draw(ht, hegel.OneOf(
			hegel.Binary(0, 64),
			hegel.Map(hegel.Lists(logChunk).MaxSize(4), func(parts []string) []byte {
				return []byte("2021-05-26T12:37:00.123Z " + strings.Join(parts, ""))
			}),
		))
		entry, err := servicelog.Parse(line)
		if err != nil {
			return
		}
		ts, _, _ := strings.Cut(string(line), " ")
		if want := ts + " [" + entry.Service + "] " + entry.Message; string(line) != want {
			ht.Fatalf("Parse(%q) = %+v, which does not reconstruct the line", line, entry)
		}
	})
}
