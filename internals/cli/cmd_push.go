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

	"github.com/canonical/go-flags"
	"github.com/canonical/pebble/client"
)

const cmdPushSummary = "Transfer a file to the remote system"
const cmdPushDescription = `
The push command transfers a file to the remote system.
`

type cmdPush struct {
	clientMixin

	MakeDirs    bool   `short:"p"`
	Permissions string `short:"m"`
	UserID      *int   `long:"uid"`
	User        string `long:"user"`
	GroupID     *int   `long:"gid"`
	Group       string `long:"group"`

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
			"-m":      "Set permissions for the file (e.g. 0644)",
			"--uid":   "Use specified user ID",
			"--user":  "Use specified username",
			"--gid":   "Use specified group ID",
			"--group": "Use specified group name",
		},
		Builder: func() flags.Commander { return &cmdPush{} },
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

	st, err := f.Stat()
	if err != nil {
		return err
	}

	var permissions string
	if cmd.Permissions != "" {
		permissions = cmd.Permissions
	} else {
		permissions = fmt.Sprintf("%03o", st.Mode().Perm())
	}

	return cmd.client.Push(&client.PushOptions{
		Source:      f,
		Path:        cmd.Positional.RemotePath,
		MakeDirs:    cmd.MakeDirs,
		Permissions: permissions,
		UserID:      cmd.UserID,
		User:        cmd.User,
		GroupID:     cmd.GroupID,
		Group:       cmd.Group,
	})
}
