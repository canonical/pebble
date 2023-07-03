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
	"fmt"
	"os"
	"strconv"

	"github.com/canonical/go-flags"

	"github.com/canonical/pebble/client"
)

const cmdMkdirSummary = "Create a directory"
const cmdMkdirDescription = "The mkdir command creates the specified directory."

type cmdMkdir struct {
	clientMixin

	MakeParents bool   `short:"p"`
	Permissions string `short:"m"`
	UserID      *int   `long:"uid"`
	User        string `long:"user"`
	GroupID     *int   `long:"gid"`
	Group       string `long:"group"`

	Positional struct {
		Path string `positional-arg-name:"<path>"`
	} `positional-args:"yes" required:"yes"`
}

func init() {
	AddCommand(&CmdInfo{
		Name:        "mkdir",
		Summary:     cmdMkdirSummary,
		Description: cmdMkdirDescription,
		Builder:     func() flags.Commander { return &cmdMkdir{} },
		ArgsHelp: map[string]string{
			"-p":      "Create parent directories as needed",
			"-m":      "Set permissions (e.g. 0644)",
			"--uid":   "Use specified user ID",
			"--user":  "Use specified username",
			"--gid":   "Use specified group ID",
			"--group": "Use specified group name",
		},
	})
}

func (cmd *cmdMkdir) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	opts := client.MakeDirOptions{
		Path:        cmd.Positional.Path,
		MakeParents: cmd.MakeParents,
		UserID:      cmd.UserID,
		User:        cmd.User,
		GroupID:     cmd.GroupID,
		Group:       cmd.Group,
	}

	if cmd.Permissions != "" {
		p, err := strconv.ParseUint(cmd.Permissions, 8, 32)
		if err != nil {
			return fmt.Errorf("invalid mode for directory: %q", cmd.Permissions)
		}
		opts.Permissions = os.FileMode(p)
	}

	return cmd.client.MakeDir(&opts)
}
