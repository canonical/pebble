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
	"strconv"

	"github.com/jessevdk/go-flags"

	"github.com/canonical/pebble/client"
)

type cmdMkdir struct {
	clientMixin

	MakeParents bool   `short:"p" long:"parents"`
	Permissions string `short:"m" long:"mode"`
	User        string `short:"u" long:"user"`
	Group       string `short:"g" long:"group"`

	Positional struct {
		Path string `positional-arg-name:"<path>"`
	} `positional-args:"yes" required:"yes"`
}

var mkdirDescs = map[string]string{
	"parents": "Create parent directories as needed.",
	"mode":    "Set permissions for the newly created directories (in 3-digit octal format).",
	"user":    "Set owner user name or ID.",
	"group":   "Set owner group name or ID.",
}

var shortMkdirHelp = "Create a directory or directory tree"
var longMkdirHelp = `
The mkdir command creates a directory on the specified path.
If --parents is specified, create a directory tree.
`

func (cmd *cmdMkdir) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	opts := client.MakeDirOptions{
		Path:        cmd.Positional.Path,
		MakeParents: cmd.MakeParents,
	}

	if cmd.Permissions != "" {
		p, err := strconv.ParseUint(cmd.Permissions, 8, 32)
		if err != nil {
			return fmt.Errorf("error parsing permissions: %w", err)
		}
		mode := os.FileMode(p)
		opts.Permissions = &mode
	}

	if cmd.User != "" {
		uid, err := strconv.Atoi(cmd.User)
		if err != nil {
			// Not a number
			opts.User = cmd.User
		} else {
			opts.UserID = &uid
		}
	}

	if cmd.Group != "" {
		gid, err := strconv.Atoi(cmd.Group)
		if err != nil {
			// Not a number
			opts.Group = cmd.Group
		} else {
			opts.GroupID = &gid
		}
	}

	err := cmd.client.MakeDir(&opts)
	if err != nil {
		return err
	}

	return nil
}

func init() {
	addCommand("mkdir", shortMkdirHelp, longMkdirHelp, func() flags.Commander { return &cmdMkdir{} }, mkdirDescs, nil)
}
