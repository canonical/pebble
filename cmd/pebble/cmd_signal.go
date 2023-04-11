// Copyright (c) 2021 Canonical Ltd
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
	"fmt"
	"strings"

	"github.com/canonical/go-flags"

	"github.com/canonical/pebble/client"
)

type cmdSignal struct {
	clientMixin
	Positional struct {
		Signal   string   `positional-arg-name:"<SIGNAL>"`
		Services []string `positional-arg-name:"<service>"`
	} `positional-args:"yes" required:"yes"`
}

var shortSignalHelp = "Send a signal to one or more running services"
var longSignalHelp = `
The signal command sends a signal to one or more running services. The signal
name must be uppercase, for example:

pebble signal HUP mysql nginx
`

func (cmd *cmdSignal) Execute(args []string) error {
	if strings.ToUpper(cmd.Positional.Signal) != cmd.Positional.Signal {
		return fmt.Errorf("signal name must be uppercase, for example HUP")
	}
	if !strings.HasPrefix(cmd.Positional.Signal, "SIG") {
		cmd.Positional.Signal = "SIG" + cmd.Positional.Signal
	}
	opts := client.SendSignalOptions{
		Signal:   cmd.Positional.Signal,
		Services: cmd.Positional.Services,
	}
	err := cmd.client.SendSignal(&opts)
	if err != nil {
		return err
	}
	return nil
}

func init() {
	addCommand("signal", shortSignalHelp, longSignalHelp, func() flags.Commander { return &cmdSignal{} }, nil, nil)
}
