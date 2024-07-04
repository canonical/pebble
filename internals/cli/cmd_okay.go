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

	"github.com/canonical/pebble/client"
	cmdpkg "github.com/canonical/pebble/cmd"
)

const cmdOkaySummary = "Acknowledge notices"
const cmdOkayDescription = `
The okay command acknowledges notices that have been previously listed using
'{{.ProgramName}} notices', so that they are omitted from future runs of the command.
When a notice is repeated, it will show up again until the next '{{.ProgramName}} okay'.
`

type cmdOkay struct {
	client *client.Client

	socketPath string
}

func init() {
	AddCommand(&CmdInfo{
		Name:        "okay",
		Summary:     cmdOkaySummary,
		Description: cmdOkayDescription,
		New: func(opts *CmdOptions) flags.Commander {
			return &cmdOkay{
				client:     opts.Client,
				socketPath: opts.SocketPath,
			}
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
	if !state.NoticesLastListed.IsZero() {
		state.NoticesLastOkayed = state.NoticesLastListed
		err = saveCLIState(cmd.socketPath, state)
		if err != nil {
			return fmt.Errorf("cannot save CLI state: %w", err)
		}
	} else {
		return fmt.Errorf("no notices have been listed; try '%s notices'", cmdpkg.ProgramName)
	}

	return nil
}
