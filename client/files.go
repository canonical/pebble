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

package client

import (
	"errors"
	"io/fs"
	"net/url"
	"os"
	"strconv"
	"time"
)

type ListFilesOptions struct {
	Path    string
	Pattern string // optional
	Itself  bool   // optional
}

type fileInfo struct {
	name            string
	size            int64
	mode            fs.FileMode
	modTime         time.Time
	path            string
	userID, groupID int
	user, group     string
}

func (fi fileInfo) Name() string {
	return fi.name
}

func (fi fileInfo) Size() int64 {
	return fi.size
}

func (fi fileInfo) Mode() fs.FileMode {
	return fi.mode
}

func (fi fileInfo) ModTime() time.Time {
	return fi.modTime
}

func (fi fileInfo) IsDir() bool {
	return fi.mode.IsDir()
}

func (fi fileInfo) Sys() interface{} {
	return nil
}

func (fi fileInfo) Path() string {
	return fi.path
}

func (fi fileInfo) UserID() int {
	return fi.userID
}

func (fi fileInfo) GroupID() int {
	return fi.groupID
}

func (fi fileInfo) User() string {
	return fi.user
}

func (fi fileInfo) Group() string {
	return fi.group
}

type FileInfo interface {
	os.FileInfo

	Path() string
	UserID() int
	GroupID() int
	User() string
	Group() string
}

type fileInfoResult struct {
	Path         string `json:"path"`
	Name         string `json:"name"`
	Type         string `json:"type"`
	Size         *int64 `json:"size,omitempty"`
	Permissions  string `json:"permissions"`
	LastModified string `json:"last-modified"`
	UserID       *int   `json:"user-id"`
	User         string `json:"user"`
	GroupID      *int   `json:"group-id"`
	Group        string `json:"group"`
}

func calculateFileMode(fileType string, permissions string) (mode os.FileMode, err error) {
	p, err := strconv.ParseUint(permissions, 8, 32)
	if err != nil {
		return 0, err
	}

	mode = os.FileMode(p)
	switch fileType {
	case "file":
		mode |= os.ModeType
	case "directory":
		mode |= os.ModeDir
	case "symlink":
		mode |= os.ModeSymlink
	case "socket":
		mode |= os.ModeSocket
	case "named-pipe":
		mode |= os.ModeNamedPipe
	case "device":
		mode |= os.ModeDevice
	default:
		return 0, errors.New("invalid file type")
	}

	return mode, nil
}

func resultToFileInfo(result fileInfoResult) (FileInfo, error) {
	fi := fileInfo{}

	mode, err := calculateFileMode(result.Type, result.Permissions)
	if err != nil {
		return fi, err
	}

	fi.modTime, err = time.Parse(time.RFC3339, result.LastModified)
	if err != nil {
		return fi, err
	}

	if result.Size != nil {
		fi.size = *result.Size
	}
	if result.UserID != nil {
		fi.userID = *result.UserID
	}
	if result.GroupID != nil {
		fi.groupID = *result.GroupID
	}

	fi.path = result.Path
	fi.name = result.Name
	fi.mode = mode
	fi.user = result.User
	fi.group = result.Group

	return fi, nil
}

// ListFiles obtains the contents of a directory or glob, or information about a file.
func (client *Client) ListFiles(opts *ListFilesOptions) ([]FileInfo, error) {
	q := make(url.Values)
	q.Set("action", "list")
	q.Set("path", opts.Path)
	if opts.Pattern != "" {
		q.Set("pattern", opts.Pattern)
	}
	if opts.Itself {
		q.Set("itself", "true")
	}

	var results []fileInfoResult
	_, err := client.doSync("GET", "/v1/files", q, nil, nil, &results)
	if err != nil {
		return nil, err
	}

	infos := make([]FileInfo, len(results))
	for i, result := range results {
		infos[i], err = resultToFileInfo(result)
		if err != nil {
			return nil, err
		}
	}

	return infos, nil
}
