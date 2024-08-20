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

package cli

import (
	"fmt"
	"io"
	"strings"
	"time"
	"unicode"

	"github.com/canonical/go-flags"
	"github.com/canonical/x-go/strutil/quantity"

	"github.com/canonical/pebble/client"
	"github.com/canonical/pebble/cmd"
)

const cmdWarningsSummary = "List warnings"
const cmdWarningsDescription = `
The warnings command lists the warnings that have been reported to the system.

Once warnings have been listed with '{{.ProgramName}} warnings', '{{.ProgramName}} okay' may be
used to silence them. A warning that's been silenced in this way will not be
listed again unless it happens again, _and_ a cooldown time has passed.

Warnings expire automatically, and once expired they are forgotten.
`

type cmdWarnings struct {
	client *client.Client

	socketPath string

	timeMixin
	unicodeMixin
	All     bool `long:"all"`
	Verbose bool `long:"verbose"`
}

func init() {
	AddCommand(&CmdInfo{
		Name:        "warnings",
		Summary:     cmdWarningsSummary,
		Description: cmdWarningsDescription,
		ArgsHelp: merge(timeArgsHelp, unicodeArgsHelp, map[string]string{
			"--all":     "Show all warnings",
			"--verbose": "Show more information",
		}),
		New: func(opts *CmdOptions) flags.Commander {
			return &cmdWarnings{
				client:     opts.Client,
				socketPath: opts.SocketPath,
			}
		},
	})
}

func (cmd *cmdWarnings) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	options := &client.NoticesOptions{
		Types: []client.NoticeType{client.WarningNotice},
	}
	var state *cliState
	if !cmd.All {
		var err error
		state, err = loadCLIState(cmd.socketPath)
		if err != nil {
			return fmt.Errorf("cannot load CLI state: %w", err)
		}
		options.After = state.WarningsLastOkayed
	}

	warnings, err := cmd.client.Notices(options)
	if len(warnings) == 0 {
		if cmd.All || state.WarningsLastOkayed.IsZero() {
			fmt.Fprintln(Stderr, "No warnings.")
		} else {
			fmt.Fprintln(Stderr, "No further warnings.")
		}
		return nil
	}

	termWidth, _ := termSize()
	if termWidth > 100 {
		// any wider than this and it gets hard to read
		termWidth = 100
	}

	w := tabWriter()
	for i, warning := range warnings {
		if i > 0 {
			fmt.Fprintln(w, "---")
		}
		if cmd.Verbose {
			fmt.Fprintf(w, "first-occurrence:\t%s\n", cmd.fmtTime(warning.FirstOccurred))
		}
		fmt.Fprintf(w, "last-occurrence:\t%s\n", cmd.fmtTime(warning.LastOccurred))
		if cmd.Verbose {
			fmt.Fprintf(w, "last-repeated:\t%s\n", cmd.fmtTime(warning.LastRepeated))
			// TODO: cmd.fmtDuration() using timeutil.HumanDuration
			fmt.Fprintf(w, "repeats-after:\t%s\n", quantity.FormatDuration(warning.RepeatAfter.Seconds()))
			fmt.Fprintf(w, "expires-after:\t%s\n", quantity.FormatDuration(warning.ExpireAfter.Seconds()))
		}
		fmt.Fprintln(w, "warning: |")
		writeWarning(w, warning.Key, termWidth)
		w.Flush()
	}

	if !cmd.All {
		state.WarningsLastListed = warnings[len(warnings)-1].LastRepeated
		err = saveCLIState(cmd.socketPath, state)
		if err != nil {
			return fmt.Errorf("cannot save CLI state: %w", err)
		}
	}

	return nil
}

// writeWarning formats and writes descr to w.
//
// The behavior is:
// - trim trailing whitespace
// - word wrap at "max" chars preserving line indent
// - keep \n intact and break there
func writeWarning(w io.Writer, descr string, termWidth int) error {
	var err error
	descr = strings.TrimRightFunc(descr, unicode.IsSpace)
	for _, line := range strings.Split(descr, "\n") {
		err = wrapLine(w, []rune(line), "  ", termWidth)
		if err != nil {
			break
		}
	}
	return err
}

func maybePresentWarnings(lastListed, latest time.Time) {
	if latest.IsZero() {
		return
	}

	if !latest.After(lastListed) {
		return
	}

	fmt.Fprintf(Stderr, "WARNING: There are new warnings. See '%s warnings'.\n", cmd.ProgramName)
}
