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
)

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

var pushDescs = map[string]string{
	"p":     "Create parent directories for this file.",
	"m":     "Set permissions for the file in the remote system (in octal format).",
	"uid":   "Set owner user ID.",
	"user":  "Set owner user name.",
	"gid":   "Set owner group ID.",
	"group": "Set owner group name.",
}

var shortPushHelp = "Transfer a file to the remote system"
var longPushHelp = `
The push command transfers a file to the remote system.
`

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

	err = cmd.client.Push(&client.PushOptions{
		Source:      f,
		Path:        cmd.Positional.RemotePath,
		MakeDirs:    cmd.MakeDirs,
		Permissions: permissions,
		UserID:      cmd.UserID,
		User:        cmd.User,
		GroupID:     cmd.GroupID,
		Group:       cmd.Group,
	})
	if err != nil {
		return err
	}

	return nil
}

func init() {
	addCommand("push", shortPushHelp, longPushHelp, func() flags.Commander { return &cmdPush{} }, pushDescs, nil)
}
