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

const cmdStartSummary = "Start a service and its dependencies"
const cmdStartDescription = `
The start command starts the service with the provided name and
any other services it depends on, in the correct order.
`

type cmdStart struct {
	client *client.Client

	waitMixin
	Positional struct {
		Services []string `positional-arg-name:"<service>" required:"1"`
	} `positional-args:"yes"`
}

func init() {
	AddCommand(&CmdInfo{
		Name:        "start",
		Summary:     cmdStartSummary,
		Description: cmdStartDescription,
		ArgsHelp:    waitArgsHelp,
		New: func(opts *CmdOptions) flags.Commander {
			return &cmdStart{client: opts.Client}
		},
	})
}

func (cmd cmdStart) Execute(args []string) error {
	if len(args) > 1 {
		return ErrExtraArgs
	}

	servopts := client.ServiceOptions{
		Names: cmd.Positional.Services,
	}
	changeID, err := cmd.client.Start(&servopts)
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
