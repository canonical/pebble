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

package main

import (
	"github.com/jessevdk/go-flags"

	"github.com/canonical/pebble/client"
)

type cmdRm struct {
	clientMixin

	Recursive bool `short:"r" long:"recursive"`

	Positional struct {
		Path string `positional-arg-name:"<path>"`
	} `positional-args:"yes" required:"yes"`
}

var rmDescs = map[string]string{
	"recursive": "Create parent directories as needed.",
}

var shortRmHelp = "Remove files and directories"
var longRmHelp = `
The rm command removes a file or directory.
If --recursive is specified, all files and directories contained
within the specified path will also be removed.
`

func (cmd *cmdRm) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	return cmd.client.RemovePath(&client.RemovePathOptions{
		Path:      cmd.Positional.Path,
		Recursive: cmd.Recursive,
	})
}

func init() {
	addCommand("rm", shortRmHelp, longRmHelp, func() flags.Commander { return &cmdRm{} }, rmDescs, nil)
}
