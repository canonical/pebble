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
	"path/filepath"
	"time"

	"github.com/jessevdk/go-flags"

	"github.com/canonical/pebble/client"
	"github.com/canonical/pebble/internal/progress"
	"github.com/canonical/pebble/internal/strutil/quantity"
)

type cmdPush struct {
	clientMixin

	MakeDirs    bool   `short:"p" long:"parents"`
	Permissions string `short:"m" long:"mode"`
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
	"parents": "Create parent directories for this file.",
	"mode":    "Set permissions for the file in the remote system (in octal format).",
	"uid":     "Set owner user ID.",
	"user":    "Set owner user name.",
	"gid":     "Set owner group ID.",
	"group":   "Set owner group name.",
}

var shortPushHelp = "Transfer a file to the remote system"
var longPushHelp = `
The push command transfers a file to the remote system.
`

type pushProgress struct {
	file    *os.File
	size    int64
	current int64
	pb      progress.Meter
	started time.Time
}

func (p *pushProgress) Read(b []byte) (n int, err error) {
	n, err = p.file.Read(b)
	if err != nil {
		return
	}

	p.current += int64(n)
	p.pb.Set(float64(p.current))

	if p.current == p.size {
		p.pb.Finished()

		size := quantity.FormatAmount(uint64(p.size), 0)
		duration := quantity.FormatDuration(time.Since(p.started).Seconds())
		p.pb.Notify(fmt.Sprintf("Transferred %sB in %s", size, duration))
	}

	return
}

func newPushProgress(f *os.File, remotePath string) (*pushProgress, error) {
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}

	p := &pushProgress{
		file:    f,
		size:    st.Size(),
		pb:      progress.MakeProgressBar(),
		started: time.Now(),
	}

	msg := fmt.Sprintf("Transferring %s -> %s", filepath.Base(f.Name()), remotePath)
	p.pb.Start(msg, float64(p.size))

	return p, nil
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

	p, err := newPushProgress(f, cmd.Positional.RemotePath)
	if err != nil {
		return err
	}

	return cmd.client.Push(&client.PushOptions{
		Source:      p,
		Path:        cmd.Positional.RemotePath,
		MakeDirs:    cmd.MakeDirs,
		Permissions: cmd.Permissions,
		UserID:      cmd.UserID,
		User:        cmd.User,
		GroupID:     cmd.GroupID,
		Group:       cmd.Group,
	})
}

func init() {
	addCommand("push", shortPushHelp, longPushHelp, func() flags.Commander { return &cmdPush{} }, pushDescs, nil)
}
