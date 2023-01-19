//go:build !termus
// +build !termus

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

package main

import (
	"errors"

	"github.com/jessevdk/go-flags"
)

type cmdBootFirmware struct {
	clientMixin

	Force   bool `long:"force"`
	Verbose bool `short:"v" long:"verbose"`
}

var bootFirmwareDescs = map[string]string{
	"force":   `Skip all checks`,
	"verbose": `Log all output from services to stdout`,
}

var shortBootFirmwareHelp = `Bootstrap a system with Pebble running as PID 1`

var longBootFirmwareHelp = `
The boot-firmware command performs checks on the running system, prepares the
environment to get a working system, and starts the Pebble daemon.
`

func (rcmd *cmdBootFirmware) Execute(args []string) error {
	if len(args) > 1 {
		return ErrExtraArgs
	}

	return errors.New("cannot bootstrap an unsupported platform")
}

func init() {
	addCommand("boot-firmware", shortBootFirmwareHelp, longBootFirmwareHelp, func() flags.Commander { return &cmdBootFirmware{} }, bootFirmwareDescs, nil)
}
