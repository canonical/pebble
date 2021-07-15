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

package main

import (
	"github.com/jessevdk/go-flags"

	"github.com/canonical/pebble/client"
)

var shortRestartHelp = "Restart a service and its dependencies"
var longRestartHelp = `
The restart command restarts the service with the provided name and
any other services it depends on, in the correct order.
`

type cmdRestart struct {
	waitMixin
	Positional struct {
		Services []string `positional-arg-name:"<service>" required:"1"`
	} `positional-args:"yes"`
}

func init() {
	addCommand("restart", shortRestartHelp, longRestartHelp, func() flags.Commander { return &cmdRestart{} }, nil, nil)
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

	if _, err := cmd.wait(changeID); err != nil {
		if err == noWait {
			return nil
		}
		return err
	}
	return nil
}
