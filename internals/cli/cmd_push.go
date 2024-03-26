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

const cmdPushSummary = "Transfer a file to the remote system"
const cmdPushDescription = `
The push command transfers a file to the remote system.
`

type cmdPush struct {
	client *client.Client

	Parents bool   `short:"p"`
	Mode    string `short:"m"`
	UserID  *int   `long:"uid"`
	User    string `long:"user"`
	GroupID *int   `long:"gid"`
	Group   string `long:"group"`

	Positional struct {
		LocalPath  string `positional-arg-name:"<local-path>" required:"1"`
		RemotePath string `positional-arg-name:"<remote-path>" required:"1"`
	} `positional-args:"yes"`
}

func init() {
	AddCommand(&CmdInfo{
		Name:        "push",
		Summary:     cmdPushSummary,
		Description: cmdPushDescription,
		ArgsHelp: map[string]string{
			"-p":      "Create parent directories for the file",
			"-m":      "Override mode bits (3-digit octal)",
			"--uid":   "Use specified user ID",
			"--user":  "Use specified username",
			"--gid":   "Use specified group ID",
			"--group": "Use specified group name",
		},
		New: func(opts *CmdOptions) flags.Commander {
			return &cmdPush{client: opts.Client}
		},
	})
}
func (cmd *cmdPush) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	f, err := os.Open(cmd.Positional.LocalPath)
	if err != nil {
		return err
	}
	defer f.Close()

	var permissions os.FileMode
	if cmd.Mode != "" {
		p, err := strconv.ParseUint(cmd.Mode, 8, 32)
		if err != nil {
			return fmt.Errorf("invalid mode for directory: %q", cmd.Mode)
		}
		permissions = os.FileMode(p)
	} else {
		st, err := f.Stat()
		if err != nil {
			return err
		}
		permissions = st.Mode().Perm()
	}

	return cmd.client.Push(&client.PushOptions{
		Source:      f,
		Path:        cmd.Positional.RemotePath,
		MakeDirs:    cmd.Parents,
		Permissions: permissions,
		UserID:      cmd.UserID,
		User:        cmd.User,
		GroupID:     cmd.GroupID,
		Group:       cmd.Group,
	})
}
