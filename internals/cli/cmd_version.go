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
	"fmt"

	"github.com/canonical/go-flags"

	"github.com/canonical/pebble/client"
	cmdpkg "github.com/canonical/pebble/cmd"
)

var shortVersionHelp = "Show version details"
var longVersionHelp = `
The version command displays the versions of the running client and server.
`

type cmdVersion struct {
	clientMixin
	ClientOnly bool `long:"client"`
}

var versionDescs = map[string]string{
	"client": `Only display the client version`,
}

func init() {
	AddCommand(CmdInfo{
		Name:      "version",
		ShortHelp: shortVersionHelp,
		LongHelp:  longVersionHelp,
		Builder:   func() flags.Commander { return &cmdVersion{} },
		OptDescs:  versionDescs,
	})
}

func (cmd cmdVersion) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	if cmd.ClientOnly {
		fmt.Fprintln(Stdout, cmdpkg.Version)
		return nil
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
	fmt.Fprintf(w, "client\t%s\n", cmdpkg.Version)
	fmt.Fprintf(w, "server\t%s\n", serverVersion)
	w.Flush()
	return nil
}
