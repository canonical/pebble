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
	"errors"
	"fmt"
	pathpkg "path"
	"strings"

	"github.com/canonical/go-flags"
	"github.com/canonical/x-go/strutil/quantity"

	"github.com/canonical/pebble/client"
)

const cmdLsSummary = "List path contents"
const cmdLsDescription = `
The ls command lists entries in the filesystem at the specified path. A glob pattern
may be specified for the last path element.
`

type cmdLs struct {
	client *client.Client

	timeMixin
	Directory  bool `short:"d"`
	LongFormat bool `short:"l"`
	Positional struct {
		Path string `positional-arg-name:"<path>"`
	} `positional-args:"yes" required:"yes"`
}

func init() {
	AddCommand(&CmdInfo{
		Name:        "ls",
		Summary:     cmdLsSummary,
		Description: cmdLsDescription,
		ArgsHelp: merge(timeArgsHelp, map[string]string{
			"-d": "List matching entries themselves, not directory contents",
			"-l": "Use a long listing format",
		}),
		New: func(opts *CmdOptions) flags.Commander {
			return &cmdLs{client: opts.Client}
		},
	})
}

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

func parseGlob(path string) (parsedPath, parsedPattern string, err error) {
	dir, file := pathpkg.Split(strings.TrimRight(path, "/"))

	const patternCharacters = "*?["
	if strings.ContainsAny(dir, patternCharacters) {
		return "", "", errors.New("can only use globs on the last path element")
	}

	if strings.ContainsAny(file, patternCharacters) {
		return dir, file, nil
	}

	return path, "", nil
}
