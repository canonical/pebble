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
	"os"

	"github.com/canonical/go-flags"

	"github.com/canonical/pebble/client"
	"github.com/canonical/pebble/internals/logger"
)

const cmdPullSummary = "Retrieve a file from the remote system"
const cmdPullDescription = `
The pull command retrieves a file from the remote system.
`

type cmdPull struct {
	client *client.Client

	Positional struct {
		RemotePath string `positional-arg-name:"<remote-path>" required:"1"`
		LocalPath  string `positional-arg-name:"<local-path>" required:"1"`
	} `positional-args:"yes"`
}

func init() {
	AddCommand(&CmdInfo{
		Name:        "pull",
		Summary:     cmdPullSummary,
		Description: cmdPullDescription,
		New: func(opts *CmdOptions) flags.Commander {
			return &cmdPull{client: opts.Client}
		},
	})
}

func (cmd *cmdPull) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	f, err := os.Create(cmd.Positional.LocalPath)
	if err != nil {
		return err
	}
	defer f.Close()

	err = cmd.client.Pull(&client.PullOptions{
		Path:   cmd.Positional.RemotePath,
		Target: f,
	})
	if err != nil {
		// Discard file (we could have written data to it)
		if err := os.Remove(f.Name()); err != nil {
			logger.Noticef("Cannot discard pulled file: %s", err)
		}
		return err
	}

	return nil
}
