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

var shortAutoStartHelp = "Start services set to start by default"
var longAutoStartHelp = `
The autostart command starts the services that were configured
to start by default.
`

type cmdAutoStart struct {
	waitMixin
}

func init() {
	addCommand("autostart", shortAutoStartHelp, longAutoStartHelp, func() flags.Commander { return &cmdAutoStart{} }, waitDescs, nil)
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

	if _, err := cmd.wait(changeID); err != nil {
		if err == noWait {
			return nil
		}
		return err
	}
	return nil
}
