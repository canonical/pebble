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
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/canonical/go-flags"
	"github.com/canonical/x-go/strutil/quantity"

	"github.com/canonical/pebble/client"
	"github.com/canonical/pebble/cmd"
	"github.com/canonical/pebble/internals/osutil"
)

const cmdWarningsSummary = "List warnings"
const cmdWarningsDescription = `
The warnings command lists the warnings that have been reported to the system.

Once warnings have been listed with '{{.ProgramName}} warnings', '{{.ProgramName}} okay' may be used to
silence them. A warning that's been silenced in this way will not be listed
again unless it happens again, _and_ a cooldown time has passed.

Warnings expire automatically, and once expired they are forgotten.
`

type cmdWarnings struct {
	client *client.Client

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
			return &cmdWarnings{client: opts.Client}
		},
	})
}

func (cmd *cmdWarnings) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}
	now := time.Now()

	warnings, err := cmd.client.Warnings(client.WarningsOptions{All: cmd.All})
	if err != nil {
		return err
	}
	if len(warnings) == 0 {
		if t, _ := lastWarningTimestamp(); t.IsZero() {
			fmt.Fprintln(Stdout, "No warnings.")
		} else {
			fmt.Fprintln(Stdout, "No further warnings.")
		}
		return nil
	}

	if err := writeWarningTimestamp(now); err != nil {
		return err
	}

	termWidth, _ := termSize()
	if termWidth > 100 {
		// any wider than this and it gets hard to read
		termWidth = 100
	}

	esc := cmd.getEscapes()
	w := tabWriter()
	for i, warning := range warnings {
		if i > 0 {
			fmt.Fprintln(w, "---")
		}
		if cmd.Verbose {
			fmt.Fprintf(w, "first-occurrence:\t%s\n", cmd.fmtTime(warning.FirstAdded))
		}
		fmt.Fprintf(w, "last-occurrence:\t%s\n", cmd.fmtTime(warning.LastAdded))
		if cmd.Verbose {
			lastShown := esc.dash
			if !warning.LastShown.IsZero() {
				lastShown = cmd.fmtTime(warning.LastShown)
			}
			fmt.Fprintf(w, "acknowledged:\t%s\n", lastShown)
			// TODO: cmd.fmtDuration() using timeutil.HumanDuration
			fmt.Fprintf(w, "repeats-after:\t%s\n", quantity.FormatDuration(warning.RepeatAfter.Seconds()))
			fmt.Fprintf(w, "expires-after:\t%s\n", quantity.FormatDuration(warning.ExpireAfter.Seconds()))
		}
		fmt.Fprintln(w, "warning: |")
		writeWarning(w, warning.Message, termWidth)
		w.Flush()
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

const warnFileEnvKey = "PEBBLE_LAST_WARNING_TIMESTAMP_FILENAME"

func warnFilename(homedir string) string {
	if fn := os.Getenv(warnFileEnvKey); fn != "" {
		return fn
	}

	return filepath.Join(homedir, ".pebble", "warnings.json")
}

type clientWarningData struct {
	Timestamp time.Time `json:"timestamp"`
}

func writeWarningTimestamp(t time.Time) error {
	user, err := osutil.RealUser()
	if err != nil {
		return err
	}
	uid, gid, err := osutil.UidGid(user)
	if err != nil {
		return err
	}

	// FIXME Keep track of this data on a per-user+per-pebble socket basis.
	filename := warnFilename(user.HomeDir)
	err = osutil.Mkdir(filepath.Dir(filename), 0700, &osutil.MkdirOptions{
		MakeParents: true,
		ExistOK:     true,
		Chown:       true,
		UserID:      uid,
		GroupID:     gid,
	})
	if err != nil {
		return err
	}

	aw, err := osutil.NewAtomicFile(filename, 0600, 0, uid, gid)
	if err != nil {
		return err
	}
	// Cancel once Committed is a NOP :-)
	defer aw.Cancel()

	enc := json.NewEncoder(aw)
	if err := enc.Encode(clientWarningData{Timestamp: t}); err != nil {
		return err
	}

	return aw.Commit()
}

func lastWarningTimestamp() (time.Time, error) {
	user, err := osutil.RealUser()
	if err != nil {
		return time.Time{}, fmt.Errorf("cannot determine real user: %v", err)
	}

	// FIXME Keep track of this data on a per-socket basis.
	f, err := os.Open(warnFilename(user.HomeDir))
	if err != nil {
		if os.IsNotExist(err) {
			return time.Time{}, nil
		}
		return time.Time{}, fmt.Errorf("cannot open timestamp file: %v", err)
	}
	dec := json.NewDecoder(f)
	var d clientWarningData
	if err := dec.Decode(&d); err != nil {
		return time.Time{}, fmt.Errorf("cannot decode timestamp file: %v", err)
	}
	if dec.More() {
		return time.Time{}, fmt.Errorf("spurious extra data in timestamp file")
	}
	return d.Timestamp, nil
}

func maybePresentWarnings(count int, timestamp time.Time) {
	if count == 0 {
		return
	}

	if last, _ := lastWarningTimestamp(); !timestamp.After(last) {
		return
	}

	format := "WARNING: There are %d new warnings. See '%s warnings'.\n"
	if count == 1 {
		format = "WARNING: There is %d new warning. See '%s warnings'.\n"
	}
	fmt.Fprintf(Stderr, format, count, cmd.ProgramName)
}
