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

const cmdReplanSummary = "Ensure running services and checks match the current plan"
const cmdReplanDescription = `
The replan command starts, stops, or restarts services and checks that have
changed, so that running services and checks exactly match the desired
configuration in the current plan.
`

type cmdReplan struct {
	client *client.Client

	waitMixin
}

func init() {
	AddCommand(&CmdInfo{
		Name:        "replan",
		Summary:     cmdReplanSummary,
		Description: cmdReplanDescription,
		ArgsHelp:    waitArgsHelp,
		New: func(opts *CmdOptions) flags.Commander {
			return &cmdReplan{client: opts.Client}
		},
	})
}

func (cmd cmdReplan) Execute(args []string) error {
	if len(args) > 1 {
		return ErrExtraArgs
	}

	servopts := client.ServiceOptions{}
	changeID, err := cmd.client.Replan(&servopts)
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
