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
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/jessevdk/go-flags"

	"github.com/canonical/pebble/client"
	"github.com/canonical/pebble/internal/progress"
	"github.com/canonical/pebble/internal/strutil/quantity"
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

type pullProgress struct {
	file    *os.File
	size    int64
	current int64
	pb      progress.Meter
	started time.Time
	msg     string
}

func (p *pullProgress) Write(b []byte) (n int, err error) {
	n, err = p.file.Write(b)
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

func newPullProgress(f *os.File, remotePath string) (*pullProgress, error) {
	p := &pullProgress{
		file:    f,
		pb:      progress.MakeProgressBar(),
		started: time.Now(),
	}

	p.msg = fmt.Sprintf("Transferring %s -> %s", remotePath, filepath.Base(f.Name()))
	p.pb.Spin(p.msg)

	return p, nil
}

func (p *pullProgress) setSize(size int64) {
	p.size = size
	p.pb.Start(p.msg, float64(p.size))
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

	p, err := newPullProgress(f, cmd.Positional.RemotePath)
	if err != nil {
		return err
	}

	res, err := cmd.client.Pull(&client.PullOptions{
		Path: cmd.Positional.RemotePath,
	})
	if err != nil {
		return err
	}
	p.setSize(res.Size)

	if _, err := io.Copy(p, res.Reader); err != nil {
		return err
	}

	return nil
}

func init() {
	addCommand("pull", shortPullHelp, longPullHelp, func() flags.Commander { return &cmdPull{} }, nil, nil)
}
