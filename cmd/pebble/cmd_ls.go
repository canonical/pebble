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
	Itself     bool `long:"itself"`
	Positional struct {
		Path string `positional-arg-name:"<path>"`
	} `positional-args:"yes" required:"yes"`
}

var lsDescs = map[string]string{
	"itself": `Display information about the file system entry, instead of listing directory contents.`,
}

var shortLsHelp = "Lists path contents"
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
		Itself:  cmd.Itself,
	}
	files, err := cmd.client.ListFiles(&opts)
	if err != nil {
		return err
	}

	fmt.Println("Name\tUser\tGroup\t")
	for _, fi := range files {
		fmt.Printf("%s\t%s\t%s\n", fi.Name(), fi.User(), fi.Group())
	}
	return nil
}

func init() {
	addCommand("ls", shortLsHelp, longLsHelp, func() flags.Commander { return &cmdLs{} }, lsDescs, nil)
}
