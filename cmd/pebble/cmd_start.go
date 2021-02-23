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

package main

import (
	"github.com/canonical/pebble/client"
	"github.com/jessevdk/go-flags"
)

var shortStartHelp = "Start a service and its dependencies"
var longStartHelp = `
The start command starts the service with the provided name and
any other services it depends on, in the correct order.
`

type cmdStart struct {
	waitMixin
	Positional struct {
		Services []string `positional-arg-name:"<service>" required:"1"`
	} `positional-args:"yes"`
}

func init() {
	addCommand("start", shortStartHelp, longStartHelp, func() flags.Commander { return &cmdStart{} }, nil, nil)
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

	if _, err := cmd.wait(changeID); err != nil {
		if err == noWait {
			return nil
		}
		return err
	}
	return nil
}
