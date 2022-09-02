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
	"encoding/json"
	"fmt"
	"os"
)

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

type fileResult struct {
	Path  string       `json:"path"`
	Error *errorResult `json:"error,omitempty"`
}

type errorResult struct {
	Message string      `json:"message"`
	Kind    string      `json:"kind,omitempty"`
	Value   interface{} `json:"value,omitempty"`
}

// MakeDir creates a directory or directory tree.
// The error returned is a *Error if the request went through successfully
// but there was an OS-level error creating the directory, with the Kind
// field set to the specific error kind, for example "permission-denied".
func (client *Client) MakeDir(opts *MakeDirOptions) error {
	var permissions string
	if opts.Permissions != 0 {
		permissions = fmt.Sprintf("%03o", opts.Permissions)
	}

	payload := &makeDirPayload{
		Action: "make-dirs",
		Dirs: []makeDirsItem{
			{
				Path:        opts.Path,
				MakeParents: opts.MakeParents,
				Permissions: permissions,
				UserID:      opts.UserID,
				User:        opts.User,
				GroupID:     opts.GroupID,
				Group:       opts.Group,
			},
		},
	}

	var body bytes.Buffer
	err := json.NewEncoder(&body).Encode(&payload)
	if err != nil {
		return err
	}

	var result []fileResult
	_, err = client.doSync("POST", "/v1/files", nil, map[string]string{
		"Content-Type": "application/json",
	}, &body, &result)
	if err != nil {
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
