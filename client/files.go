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
	"fmt"
	"net/url"
	"os"
	"strconv"
	"time"
)

var _ os.FileInfo = (*FileInfo)(nil)

// ListFilesOptions holds the options for a call to ListFiles.
type ListFilesOptions struct {
	// Path is the absolute path of the file system entry to be listed.
	Path string

	// Pattern is the glob-like pattern string to filter results by. When
	// present, only file system entries with names matching this pattern will
	// be included in the call results.
	Pattern string

	// Itself, when set, will force directory entries not to be listed, but
	// instead have their information returned as if they were regular files.
	Itself bool
}

type FileInfo struct {
	name    string
	size    int64
	mode    os.FileMode
	modTime time.Time
	path    string
	userID  *int
	groupID *int
	user    string
	group   string
}

// Name returns the base name of the file.
func (fi *FileInfo) Name() string {
	return fi.name
}

// Size returns the length in bytes for regular files. For others, its
// behavior is system-dependent.
func (fi *FileInfo) Size() int64 {
	return fi.size
}

// Mode returns the file mode and permission bits.
func (fi *FileInfo) Mode() os.FileMode {
	return fi.mode
}

// ModTime returns the file modification time.
func (fi *FileInfo) ModTime() time.Time {
	return fi.modTime
}

// IsDir is an abbreviation for Mode().IsDir().
func (fi *FileInfo) IsDir() bool {
	return fi.mode.IsDir()
}

// Sys returns the underlying data source (always nil for client.FileInfo).
func (fi *FileInfo) Sys() interface{} {
	return nil
}

// Path is the full absolute path of the file.
func (fi *FileInfo) Path() string {
	return fi.path
}

// UserID is the ID of the owner user (can be nil).
func (fi *FileInfo) UserID() *int {
	return fi.userID
}

// GroupID is the ID of the owner group (can be nil).
func (fi *FileInfo) GroupID() *int {
	return fi.groupID
}

// User is the string representing the owner user name.
func (fi *FileInfo) User() string {
	return fi.user
}

// Group is the string representing the owner user group.
func (fi *FileInfo) Group() string {
	return fi.group
}

// ListFiles obtains the contents of a directory or glob, or information about a file.
func (client *Client) ListFiles(opts *ListFilesOptions) ([]*FileInfo, error) {
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

	infos := make([]*FileInfo, len(results))
	for i, result := range results {
		infos[i], err = resultToFileInfo(result)
		if err != nil {
			return nil, err
		}
	}

	return infos, nil
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
		return 0, fmt.Errorf("invalid permission bits: %q", permissions)
	}

	mode = os.FileMode(p) & os.ModePerm
	switch fileType {
	case "file":
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
		mode |= os.ModeIrregular
	}

	return mode, nil
}

func resultToFileInfo(result fileInfoResult) (*FileInfo, error) {
	fi := &FileInfo{}

	mode, err := calculateFileMode(result.Type, result.Permissions)
	if err != nil {
		return nil, fmt.Errorf("remote file %q has invalid permission bits: %q", result.Name, result.Permissions)
	}

	fi.modTime, err = time.Parse(time.RFC3339, result.LastModified)
	if err != nil {
		return nil, fmt.Errorf("remote file %q has invalid last modified time: %q", result.Name, result.LastModified)
	}

	if result.Size != nil {
		fi.size = *result.Size
	}

	fi.userID = result.UserID
	fi.groupID = result.GroupID
	fi.path = result.Path
	fi.name = result.Name
	fi.mode = mode
	fi.user = result.User
	fi.group = result.Group

	return fi, nil
}
