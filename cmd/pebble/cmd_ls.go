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
	"errors"
	"fmt"
	pathpkg "path"
	"strings"

	"github.com/jessevdk/go-flags"

	"github.com/canonical/pebble/client"
	"github.com/canonical/pebble/internal/strutil/quantity"
)

// ErrPatternOutsideFileName is returned if a pattern is found anywhere but the file name of a path
var ErrPatternOutsideFileName = errors.New("patterns can only be applied to file names, not directories")

type cmdLs struct {
	clientMixin
	timeMixin

	Directory  bool `short:"d"`
	LongFormat bool `short:"l"`

	Positional struct {
		Path string `positional-arg-name:"<path>"`
	} `positional-args:"yes" required:"yes"`
}

var lsDescs = map[string]string{
	"d": `Display information about the file system entry, instead of listing directory contents.`,
	"l": `Display file system entries in a list format.`,
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

	path, pattern, err := parseGlob(cmd.Positional.Path)
	if err != nil {
		return err
	}

	files, err := cmd.client.ListFiles(&client.ListFilesOptions{
		Path:    path,
		Pattern: pattern,
		Itself:  cmd.Directory,
	})
	if err != nil {
		return err
	}

	w := tabWriter()
	defer w.Flush()
	for _, fi := range files {
		if cmd.LongFormat {
			var size string
			if fi.Mode().IsRegular() {
				size = quantity.FormatAmount(uint64(fi.Size()), -1) + "B"
			} else {
				size = "-"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%6s\t%s\t%s\n", fi.Mode().String(), fi.User(), fi.Group(), size, cmd.fmtTime(fi.ModTime()), fi.Name())
		} else {
			fmt.Fprintln(w, fi.Name())
		}
	}

	return nil
}

func init() {
	addCommand("ls", shortLsHelp, longLsHelp, func() flags.Commander { return &cmdLs{} }, merge(lsDescs, timeDescs), nil)
}

func parseGlob(path string) (parsedPath string, parsedPattern string, err error) {
	dir, file := pathpkg.Split(strings.TrimRight(path, "/"))

	const patternCharacters = "*?["
	if strings.ContainsAny(dir, patternCharacters) {
		// Patterns can not be applied recursively, only on file names
		return "", "", ErrPatternOutsideFileName
	}

	if strings.ContainsAny(file, patternCharacters) {
		// File name contains a pattern
		return dir, file, nil
	}

	// No patterns could be extracted
	return path, "", nil
}
