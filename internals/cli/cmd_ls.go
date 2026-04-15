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
	"os"
	pathpkg "path"
	"strings"
	"time"

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
	formatMixin
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
		ArgsHelp: merge(timeArgsHelp, formatArgsHelp, map[string]string{
			"-d": "List matching entries themselves, not directory contents",
			"-l": "Use a long listing format",
		}),
		New: func(opts *CmdOptions) flags.Commander {
			return &cmdLs{client: opts.Client}
		},
	})
}

type fileEntry struct {
	Path         string `json:"path" yaml:"path"`
	Name         string `json:"name" yaml:"name"`
	Type         string `json:"type" yaml:"type"`
	Size         *int64 `json:"size,omitempty" yaml:"size,omitempty"`
	Permissions  string `json:"permissions" yaml:"permissions"`
	LastModified string `json:"last-modified" yaml:"last-modified"`
	UserID       *int   `json:"user-id,omitempty" yaml:"user-id,omitempty"`
	User         string `json:"user" yaml:"user"`
	GroupID      *int   `json:"group-id,omitempty" yaml:"group-id,omitempty"`
	Group        string `json:"group" yaml:"group"`
}

type lsResult struct {
	Files []fileEntry `json:"files" yaml:"files"`
}

func fileModeToType(mode os.FileMode) string {
	switch {
	case mode&os.ModeType == 0:
		return "file"
	case mode&os.ModeDir != 0:
		return "directory"
	case mode&os.ModeSymlink != 0:
		return "symlink"
	case mode&os.ModeSocket != 0:
		return "socket"
	case mode&os.ModeNamedPipe != 0:
		return "named-pipe"
	case mode&os.ModeDevice != 0:
		return "device"
	default:
		return "unknown"
	}
}

func fileInfoToEntry(fi *client.FileInfo) fileEntry {
	mode := fi.Mode()
	entry := fileEntry{
		Path:         fi.Path(),
		Name:         fi.Name(),
		Type:         fileModeToType(mode),
		Permissions:  fmt.Sprintf("%03o", mode.Perm()),
		LastModified: fi.ModTime().Format(time.RFC3339),
		UserID:       fi.UserID(),
		User:         fi.User(),
		GroupID:      fi.GroupID(),
		Group:        fi.Group(),
	}
	if mode.IsRegular() {
		size := fi.Size()
		entry.Size = &size
	}
	return entry
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

	if cmd.Format == "text" {
		return cmd.writeText(files)
	}

	entries := make([]fileEntry, 0, len(files))
	for _, fi := range files {
		entries = append(entries, fileInfoToEntry(fi))
	}
	return cmd.formatNonText(lsResult{Files: entries})
}

func (cmd *cmdLs) writeText(files []*client.FileInfo) error {
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
