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
	"github.com/jessevdk/go-flags"

	"github.com/canonical/pebble/client"
)

type cmdLs struct {
	clientMixin
	timeMixin

	Directory  bool `short:"d" long:"directory"`
	LongFormat bool `short:"l"`

	Positional struct {
		Path string `positional-arg-name:"<path>"`
	} `positional-args:"yes" required:"yes"`
}

var lsDescs = map[string]string{
	"directory": `Display information about the file system entry, instead of listing directory contents.`,
	"l":         `Display file system entries in a list format.`,
}

var shortLsHelp = "List path contents"
var longLsHelp = `
The ls command takes a path (a file, directory or glob) and obtains its
contents.
`

func (cmd *cmdLs) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	opts := client.ListFilesOptions{
		Path:    cmd.Positional.Path,
		Pattern: "",
		Itself:  cmd.Directory,
	}
	files, err := cmd.client.ListFiles(&opts)
	if err != nil {
		return err
	}

	w := tabWriter()
	defer w.Flush()
	for _, fi := range files {
		if cmd.LongFormat {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", fi.Mode().String(), fi.User(), fi.Group(), cmd.fmtTime(fi.ModTime()), fi.Name())
		} else {
			fmt.Fprintln(w, fi.Name())
		}
	}

	return nil
}

func init() {
	addCommand("ls", shortLsHelp, longLsHelp, func() flags.Commander { return &cmdLs{} }, merge(lsDescs, timeDescs), nil)
}
