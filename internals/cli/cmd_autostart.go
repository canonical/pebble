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
	"github.com/canonical/go-flags"

	"github.com/canonical/pebble/client"
)

const cmdAutoStartSummary = "Start services set to start by default"
const cmdAutoStartDescription = `
The autostart command starts the services that were configured
to start by default.
`

type cmdAutoStart struct {
	client *client.Client

	waitMixin
}

func init() {
	AddCommand(&CmdInfo{
		Name:        "autostart",
		Summary:     cmdAutoStartSummary,
		Description: cmdAutoStartDescription,
		ArgsHelp:    waitArgsHelp,
		New: func(opts *CmdOptions) flags.Commander {
			return &cmdAutoStart{client: opts.Client}
		},
	})
}

func (cmd cmdAutoStart) Execute(args []string) error {
	if len(args) > 1 {
		return ErrExtraArgs
	}

	servopts := client.ServiceOptions{}
	changeID, err := cmd.client.AutoStart(&servopts)
	if err != nil {
		return err
	}

	if _, err := cmd.wait(cmd.client, changeID); err != nil {
		if err == noWait {
			return nil
		}
		return err
	}
	return nil
}
