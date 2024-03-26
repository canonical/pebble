// Copyright (c) 2022 Canonical Ltd
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

const cmdRmSummary = "Remove a file or directory"
const cmdRmDescription = `
The rm command removes a file or directory.
`

type cmdRm struct {
	client *client.Client

	Recursive  bool `short:"r"`
	Positional struct {
		Path string `positional-arg-name:"<path>"`
	} `positional-args:"yes" required:"yes"`
}

func init() {
	AddCommand(&CmdInfo{
		Name:        "rm",
		Summary:     cmdRmSummary,
		Description: cmdRmDescription,
		ArgsHelp: map[string]string{
			"-r": "Remove all files and directories recursively in the specified path",
		},
		New: func(opts *CmdOptions) flags.Commander {
			return &cmdRm{client: opts.Client}
		},
	})
}

func (cmd *cmdRm) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	return cmd.client.RemovePath(&client.RemovePathOptions{
		Path:      cmd.Positional.Path,
		Recursive: cmd.Recursive,
	})
}
