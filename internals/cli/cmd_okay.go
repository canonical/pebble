// Copyright (c) 2023 Canonical Ltd
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

	"github.com/canonical/go-flags"

	cmdpkg "github.com/canonical/pebble/cmd"
)

const cmdOkaySummary = "Acknowledge notices and warnings"
const cmdOkayDescription = `
The okay command acknowledges warnings and notices that have been previously
listed using '{{.ProgramName}} warnings' or '{{.ProgramName}} notices', so that they are omitted
from future runs of either command. When a notice or warning is repeated, it
will again show up until the next '{{.ProgramName}} okay'.
`

type cmdOkay struct {
	socketPath string

	Warnings bool `long:"warnings"`
}

func init() {
	AddCommand(&CmdInfo{
		Name:        "okay",
		Summary:     cmdOkaySummary,
		Description: cmdOkayDescription,
		New: func(opts *CmdOptions) flags.Commander {
			return &cmdOkay{
				socketPath: opts.SocketPath,
			}
		},
		ArgsHelp: map[string]string{
			"--warnings": "Only acknowledge warnings, not other notices",
		},
	})
}

func (cmd *cmdOkay) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	state, err := loadCLIState(cmd.socketPath)
	if err != nil {
		return fmt.Errorf("cannot load CLI state: %w", err)
	}

	okayedWarnings := false
	if !state.WarningsLastListed.IsZero() {
		okayedWarnings = true
		state.WarningsLastOkayed = state.WarningsLastListed
	}

	okayedNotices := false
	if !cmd.Warnings {
		if !state.NoticesLastListed.IsZero() {
			okayedNotices = true
			state.NoticesLastOkayed = state.NoticesLastListed
		}
	}

	err = saveCLIState(cmd.socketPath, state)
	if err != nil {
		return fmt.Errorf("cannot save CLI state: %w", err)
	}

	if cmd.Warnings && !okayedWarnings {
		return fmt.Errorf("no warnings have been listed; try '%s warnings'", cmdpkg.ProgramName)
	}
	if !cmd.Warnings && !okayedNotices && !okayedWarnings {
		return fmt.Errorf("no notices or warnings have been listed; try '%s notices' or '%s warnings'", cmdpkg.ProgramName, cmdpkg.ProgramName)
	}

	return nil
}
