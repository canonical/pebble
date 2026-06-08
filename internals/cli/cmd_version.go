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
	version "github.com/canonical/pebble/cmd"
)

const cmdVersionSummary = "Show version details"
const cmdVersionDescription = `
The version command displays the versions of the running client and server.
`

type cmdVersion struct {
	client *client.Client

	formatMixin
	ClientOnly bool `long:"client"`
}

func init() {
	AddCommand(&CmdInfo{
		Name:        "version",
		Summary:     cmdVersionSummary,
		Description: cmdVersionDescription,
		ArgsHelp: merge(formatArgsHelp, map[string]string{
			"--client": "Only display the client version",
		}),
		New: func(opts *CmdOptions) flags.Commander {
			return &cmdVersion{client: opts.Client}
		},
	})
}

type versionResult struct {
	Client string `json:"client" yaml:"client"`
	Server string `json:"server,omitempty" yaml:"server,omitempty"`
}

func (cmd cmdVersion) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	text := cmd.Format == "text"

	if cmd.ClientOnly {
		if !text {
			return cmd.formatNonText(versionResult{Client: version.Version})
		}
		fmt.Fprintln(Stdout, version.Version)
		return nil
	}

	serverVersion := ""
	sysInfo, serverErr := cmd.client.SysInfo()
	if serverErr == nil {
		serverVersion = sysInfo.Version
	} else if text {
		serverVersion = "-"
	}

	if !text {
		err := cmd.formatNonText(versionResult{
			Client: version.Version,
			Server: serverVersion,
		})
		if serverErr != nil {
			return serverErr
		}
		return err
	}

	w := tabWriter()
	fmt.Fprintf(w, "client\t%s\n", version.Version)
	fmt.Fprintf(w, "server\t%s\n", serverVersion)
	w.Flush()
	return nil
}
