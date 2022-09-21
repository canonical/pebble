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
	"fmt"
	"os"

	"github.com/jessevdk/go-flags"

	"github.com/canonical/pebble/client"
	"github.com/canonical/pebble/internal/logger"
)

type cmdPull struct {
	clientMixin

	Positional struct {
		RemotePath string `positional-arg-name:"<remote-path>" required:"1"`
		LocalPath  string `positional-arg-name:"<local-path>" required:"1"`
	} `positional-args:"yes"`
}

var shortPullHelp = "Retrieve a file from the remote system"
var longPullHelp = `
The pull command retrieves a file from the remote system.
`

func (cmd *cmdPull) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	f, err := os.Create(cmd.Positional.LocalPath)
	if err != nil {
		return fmt.Errorf("error creating file: %w", err)
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

func init() {
	addCommand("pull", shortPullHelp, longPullHelp, func() flags.Commander { return &cmdPull{} }, nil, nil)
}