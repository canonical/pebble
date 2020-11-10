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
	"fmt"

	"github.com/jessevdk/go-flags"

	"github.com/canonical/pebble/internal/client"
	"github.com/canonical/pebble/cmd"
)

var shortVersionHelp = "Show version details"
var longVersionHelp = `
The version command displays the versions of the running client and server.
`

type cmdVersion struct {
	clientMixin
}

func init() {
	addCommand("version", shortVersionHelp, longVersionHelp, func() flags.Commander { return &cmdVersion{} }, nil, nil)
}

func (cmd cmdVersion) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	return printVersions(cmd.client)
}

func printVersions(cli *client.Client) error {
	serverVersion := "-"
	sysInfo, err := cli.SysInfo()
	if err == nil {
		serverVersion = sysInfo.Version
	}
	w := tabWriter()
	fmt.Fprintf(w, "client\t%s\n", cmd.Version)
	fmt.Fprintf(w, "server\t%s\n", serverVersion)
	w.Flush()
	return nil
}
