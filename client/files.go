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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/url"
	"os"
	"strconv"
	"time"
)

var _ os.FileInfo = (*FileInfo)(nil)

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

// MakeDirOptions holds the options for a call to MakeDir.
type MakeDirOptions struct {
	// Path is the absolute path of the directory to be created (required).
	Path string

	// MakeParents, if true, specifies that any non-existent parent directories
	// should be created. If false (the default), the call will fail if the
	// directory to be created has at least one parent directory that does not
	// exist.
	MakeParents bool

	// Permissions specifies the permission bits of the directories to be created.
	// If 0 or unset, defaults to 0755.
	Permissions os.FileMode

	// UserID indicates the user ID of the owner for the created directories.
	UserID *int

	// User indicates the user name of the owner for the created directories.
	// If used together with UserID, this value must match the name of the user
	// with that ID.
	User string

	// GroupID indicates the group ID of the owner for the created directories.
	GroupID *int

	// Group indicates the name of the owner group for the created directories.
	// If used together with GroupID, this value must match the name of the group
	// with that ID.
	Group string
}

type makeDirPayload struct {
	Action string         `json:"action"`
	Dirs   []makeDirsItem `json:"dirs"`
}

type makeDirsItem struct {
	Path        string `json:"path"`
	MakeParents bool   `json:"make-parents"`
	Permissions string `json:"permissions"`
	UserID      *int   `json:"user-id"`
	User        string `json:"user"`
	GroupID     *int   `json:"group-id"`
	Group       string `json:"group"`
}

// MakeDir creates a directory or directory tree.
func (client *Client) MakeDir(opts *MakeDirOptions) error {
	var permissions string
	if opts.Permissions != 0 {
		permissions = fmt.Sprintf("%03o", opts.Permissions)
	}

	payload := &makeDirPayload{
		Action: "make-dirs",
		Dirs: []makeDirsItem{{
			Path:        opts.Path,
			MakeParents: opts.MakeParents,
			Permissions: permissions,
			UserID:      opts.UserID,
			User:        opts.User,
			GroupID:     opts.GroupID,
			Group:       opts.Group,
		}},
	}

	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(&payload); err != nil {
		return fmt.Errorf("cannot encode JSON payload: %w", err)
	}

	var result []fileResult
	headers := map[string]string{
		"Content-Type": "application/json",
	}
	if _, err := client.doSync("POST", "/v1/files", nil, headers, &body, &result); err != nil {
		return err
	}

	if len(result) != 1 {
		return fmt.Errorf("expected exactly one result from API, got %d", len(result))
	}
	if result[0].Error != nil {
		return &Error{
			Kind:    result[0].Error.Kind,
			Value:   result[0].Error.Value,
			Message: result[0].Error.Message,
		}
	}

	return nil
}

// RemovePathOptions holds the options for a call to RemovePath.
type RemovePathOptions struct {
	// Path is the absolute path to be deleted (required).
	Path string

	// Recursive, if true, will delete all files and directories contained
	// within the specified path, recursively. Defaults to false.
	Recursive bool
}

type removePathsPayload struct {
	Action string            `json:"action"`
	Paths  []removePathsItem `json:"paths"`
}

type removePathsItem struct {
	Path      string `json:"path"`
	Recursive bool   `json:"recursive"`
}

type fileResult struct {
	Path  string `json:"path"`
	Error *Error `json:"error,omitempty"`
}

// RemovePath deletes a file or directory.
// The error returned is a *Error if the request went through successfully
// but there was an OS-level error deleting a file or directory, with the Kind
// field set to the specific error kind, for example "permission-denied".
func (client *Client) RemovePath(opts *RemovePathOptions) error {
	payload := &removePathsPayload{
		Action: "remove",
		Paths: []removePathsItem{
			{
				Path:      opts.Path,
				Recursive: opts.Recursive,
			},
		},
	}

	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(&payload); err != nil {
		return fmt.Errorf("cannot encode JSON payload: %w", err)
	}

	var result []fileResult
	headers := map[string]string{
		"Content-Type": "application/json",
	}
	if _, err := client.doSync("POST", "/v1/files", nil, headers, &body, &result); err != nil {
		return err
	}

	if len(result) != 1 {
		return fmt.Errorf("expected exactly one result from API, got %d", len(result))
	}
	if result[0].Error != nil {
		return &Error{
			Kind:    result[0].Error.Kind,
			Value:   result[0].Error.Value,
			Message: result[0].Error.Message,
		}
	}

	return nil
}

// PullOptions contains the options for a call to Pull.
type PullOptions struct {
	// Path indicates the absolute path of the file in the remote system
	// (required).
	Path string

	// Target is the destination io.Writer that will receive the data (required).
	// During a call to Pull, Target may be written to even if an error is returned.
	Target io.Writer
}

// Pull retrieves a file from the remote system.
func (client *Client) Pull(opts *PullOptions) error {
	resp, err := client.Requester().Do(context.Background(), &RequestOptions{
		Type:   RawRequest,
		Method: "GET",
		Path:   "/v1/files",
		Query: map[string][]string{
			"action": {"read"},
			"path":   {opts.Path},
		},
		Headers: map[string]string{
			"Accept": "multipart/form-data",
		},
		Body: nil,
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Obtain Content-Type to check for a multipart payload and parse its value
	// in order to obtain the multipart boundary.
	mediaType, params, err := mime.ParseMediaType(resp.Headers["Content-Type"])
	if err != nil {
		return fmt.Errorf("invalid Content-Type: %w", err)
	}
	if mediaType != "multipart/form-data" {
		// Not an error response after all.
		return fmt.Errorf("expected a multipart response, got %q", mediaType)
	}

	mr := multipart.NewReader(resp.Body, params["boundary"])
	filesPart, err := mr.NextPart()
	if err != nil {
		return fmt.Errorf("cannot decode multipart payload: %w", err)
	}
	defer filesPart.Close()

	if filesPart.FormName() != "files" {
		return fmt.Errorf(`expected first field name to be "files", got %q`, filesPart.FormName())
	}
	if _, err := io.Copy(opts.Target, filesPart); err != nil {
		return fmt.Errorf("cannot write: %w", err)
	}

	responsePart, err := mr.NextPart()
	if err != nil {
		return fmt.Errorf("cannot decode multipart payload: %w", err)
	}
	defer responsePart.Close()
	if responsePart.FormName() != "response" {
		return fmt.Errorf(`expected second field name to be "response", got %q`, responsePart.FormName())
	}

	// Process response metadata (see defaultRequester.Do() in client package)
	var multipartResp response
	if err := decodeInto(responsePart, &multipartResp); err != nil {
		return err
	}
	if err := multipartResp.err(); err != nil {
		return err
	}
	if multipartResp.Type != "sync" {
		return fmt.Errorf("expected sync response, got %q", multipartResp.Type)
	}

	requestResponse := &RequestResponse{Result: multipartResp.Result}

	// Decode response result.
	var fr []fileResult
	if err := requestResponse.DecodeResult(&fr); err != nil {
		return fmt.Errorf("cannot unmarshal result: %w", err)
	}
	if len(fr) != 1 {
		return fmt.Errorf("expected exactly one result from API, got %d", len(fr))
	}
	if fr[0].Error != nil {
		return fr[0].Error
	}
	return nil
}
