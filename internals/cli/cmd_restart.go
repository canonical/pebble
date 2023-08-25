// Copyright (c) 2014-2021 Canonical Ltd
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

const cmdRestartSummary = "Restart a service"
const cmdRestartDescription = `
The restart command restarts the named service(s) in the correct order.
`

type cmdRestart struct {
	client *client.Client

	waitMixin
	Positional struct {
		Services []string `positional-arg-name:"<service>" required:"1"`
	} `positional-args:"yes"`
}

func init() {
	AddCommand(&CmdInfo{
		Name:        "restart",
		Summary:     cmdRestartSummary,
		Description: cmdRestartDescription,
		ArgsHelp:    waitArgsHelp,
		New: func(opts *CmdOptions) flags.Commander {
			return &cmdRestart{client: opts.Client}
		},
	})
}

func (cmd cmdRestart) Execute(args []string) error {
	if len(args) > 1 {
		return ErrExtraArgs
	}

	servopts := client.ServiceOptions{
		Names: cmd.Positional.Services,
	}
	changeID, err := cmd.client.Restart(&servopts)
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
