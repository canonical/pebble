// Copyright (c) 2014-2020 Canonical Ltd
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

//lint:file-ignore U1000 Some things here are not used, but we want to keep in sync with snapd

package cli

import (
	"bytes"
	"fmt"
	"go/doc"
	"io"
	"maps"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"unicode"
	"unicode/utf8"

	"golang.org/x/term"
)

type unicodeMixin struct {
	//lint:ignore SA5008 "choice" tag is intentionally duplicated
	Unicode string `long:"unicode" default:"auto" choice:"auto" choice:"never" choice:"always"`
}

func (ux unicodeMixin) addUnicodeChars(esc *escapes) {
	if canUnicode(ux.Unicode) {
		esc.dash = "–" // that's an en dash (so yaml is happy)
		esc.uparrow = "↑"
		esc.tick = "✓"
	} else {
		esc.dash = "--" // two dashes keeps yaml happy also
		esc.uparrow = "^"
		esc.tick = "*"
	}
}

func (ux unicodeMixin) getEscapes() *escapes {
	esc := &escapes{}
	ux.addUnicodeChars(esc)
	return esc
}

func canUnicode(mode string) bool {
	switch mode {
	case "always":
		return true
	case "never":
		return false
	}
	if !isStdoutTTY {
		return false
	}
	var lang string
	for _, k := range []string{"LC_MESSAGES", "LC_ALL", "LANG"} {
		lang = os.Getenv(k)
		if lang != "" {
			break
		}
	}
	if lang == "" {
		return false
	}
	lang = strings.ToUpper(lang)
	return strings.Contains(lang, "UTF-8") || strings.Contains(lang, "UTF8")
}

func colorTable(mode string) escapes {
	switch mode {
	case "always":
		return color
	case "never":
		return noesc
	}
	if !isStdoutTTY {
		return noesc
	}
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		// from http://no-color.org/:
		//   command-line software which outputs text with ANSI color added should
		//   check for the presence of a NO_COLOR environment variable that, when
		//   present (regardless of its value), prevents the addition of ANSI color.
		return mono // bold & dim is still ok
	}
	if term := os.Getenv("TERM"); term == "xterm-mono" || term == "linux-m" {
		// these are often used to flag "I don't want to see color" more than "I can't do color"
		// (if you can't *do* color, `color` and `mono` should produce the same results)
		return mono
	}
	return color
}

var unicodeArgsHelp = map[string]string{
	"--unicode": "Use a little bit of Unicode to improve legibility.",
}

func merge(srcs ...map[string]string) map[string]string {
	count := 0
	for _, m := range srcs {
		count += len(m)
	}
	merged := make(map[string]string, count)
	for _, m := range srcs {
		maps.Copy(merged, m)
	}
	return merged
}

type escapes struct {
	green string
	bold  string
	end   string

	tick, dash, uparrow string
}

var (
	color = escapes{
		green: "\033[32m",
		bold:  "\033[1m",
		end:   "\033[0m",
	}

	mono = escapes{
		green: "\033[1m",
		bold:  "\033[1m",
		end:   "\033[0m",
	}

	noesc = escapes{}
)

func tabWriter() *tabwriter.Writer {
	return tabwriter.NewWriter(Stdout, 5, 3, 2, ' ', 0)
}

var termSize = termSizeImpl

func termSizeImpl() (width, height int) {
	if f, ok := Stdout.(*os.File); ok {
		width, height, _ = term.GetSize(int(f.Fd()))
	}

	if width <= 0 {
		width, _ = strconv.Atoi(os.Getenv("COLUMNS"))
	}

	if height <= 0 {
		height, _ = strconv.Atoi(os.Getenv("LINES"))
	}

	if width < 40 {
		width = 80
	}

	if height < 15 {
		height = 25
	}

	return width, height
}

// TODO Kill this and use wrapLine. Probably a drop-in replacement.
func fill(para string, indent int) string {
	width, _ := termSize()

	if width > 100 {
		width = 100
	}

	// some terminals aren't happy about writing in the last
	// column (they'll add line for you). We could check terminfo
	// for "sam" (semi_auto_right_margin), but that's a lot of
	// work just for this.
	width--

	var buf bytes.Buffer
	indentStr := strings.Repeat(" ", indent)
	doc.ToText(&buf, para, indentStr, indentStr, width-indent) //lint:ignore SA1019 Deprecated

	return strings.TrimSpace(buf.String())
}

// runesTrimRightSpace returns text, with any trailing whitespace dropped.
func runesTrimRightSpace(text []rune) []rune {
	j := len(text)
	for j > 0 && unicode.IsSpace(text[j-1]) {
		j--
	}
	return text[:j]
}

// runesLastIndexSpace returns the index of the last whitespace rune
// in the text. If the text has no whitespace, returns -1.
func runesLastIndexSpace(text []rune) int {
	for i := len(text) - 1; i >= 0; i-- {
		if unicode.IsSpace(text[i]) {
			return i
		}
	}
	return -1
}

// wrapLine wraps a line, assumed to be part of a block-style yaml
// string, to fit into termWidth, preserving the line's indent, and
// writes it out prepending padding to each line.
func wrapLine(out io.Writer, text []rune, pad string, termWidth int) error {
	// discard any trailing whitespace
	text = runesTrimRightSpace(text)
	// establish the indent of the whole block
	idx := 0
	for idx < len(text) && unicode.IsSpace(text[idx]) {
		idx++
	}
	indent := pad + string(text[:idx])
	text = text[idx:]
	if len(indent) > termWidth/2 {
		// If indent is too big there's not enough space for the actual
		// text, in the pathological case the indent can even be bigger
		// than the terminal which leads to lp:1828425.
		// Rather than let that happen, give up.
		indent = pad + "  "
	}
	return wrapGeneric(out, text, indent, indent, termWidth)
}

// wrapGeneric wraps the given text to the given width, prefixing the
// first line with indent and the remaining lines with indent2
func wrapGeneric(out io.Writer, text []rune, indent, indent2 string, termWidth int) error {
	// Note: this is _wrong_ for much of unicode (because the width of a rune on
	//       the terminal is anything between 0 and 2, not always 1 as this code
	//       assumes) but fixing that is Hard. Long story short, you can get close
	//       using a couple of big unicode tables (which is what wcwidth
	//       does). Getting it 100% requires a terminfo-alike of unicode behaviour.
	//       However, before this we'd count bytes instead of runes, so we'd be
	//       even more broken. Think of it as successive approximations... at least
	//       with this work we share tabwriter's opinion on the width of things!

	// This (and possibly printDescr below) should move to strutil once
	// we're happy with it getting wider (heh heh) use.

	indentWidth := utf8.RuneCountInString(indent)
	delta := indentWidth - utf8.RuneCountInString(indent2)
	width := termWidth - indentWidth

	// establish the indent of the whole block
	idx := 0
	var err error
	for len(text) > width && err == nil {
		// find a good place to chop the text
		idx = runesLastIndexSpace(text[:width+1])
		if idx < 0 {
			// there's no whitespace; just chop at line width
			idx = width
		}
		_, err = fmt.Fprint(out, indent, string(text[:idx]), "\n")
		// prune any remaining whitespace before the start of the next line
		for idx < len(text) && unicode.IsSpace(text[idx]) {
			idx++
		}
		text = text[idx:]
		width += delta
		indent = indent2
		delta = 0
	}
	if err != nil {
		return err
	}
	_, err = fmt.Fprint(out, indent, string(text), "\n")
	return err
}
