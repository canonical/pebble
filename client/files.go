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
	"errors"
	"fmt"
)

// MakeDirOptions holds the options for a call to ListFiles.
type MakeDirOptions struct {
	// Path is the absolute path of the directory to be created (required).
	Path string

	// MakeParents specifies whether the non-existing parent directories will
	// be created or not. If one of the parent directories of the specified
	// path do not exist and MakeParents is set to false, the call will fail.
	MakeParents bool

	// Permissions specifies the 3-digit octal UNIX permissions the created
	// directories will have.
	Permissions string

	// UserID indicates the ID of the owner user for the created directories.
	UserID *int

	// User indicates the name of the owner user for the created directories.
	// If used together with UserID, UserID takes precedence.
	User string

	// GroupID indicates the ID of the owner group for the created directories.
	GroupID *int

	// Group indicates the name of the owner group for the created directories.
	// If used together with GroupID, GroupID takes precedence.
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
	Message string `json:"message"`
}

// MakeDir creates a directory or directory tree.
func (client *Client) MakeDir(opts *MakeDirOptions) error {
	payload, err := convertOptionsToPayload(opts)
	if err != nil {
		return err
	}

	var body bytes.Buffer
	err = json.NewEncoder(&body).Encode(&payload)
	if err != nil {
		return err
	}

	var result []fileResult
	_, err = client.doSync("POST", "/v1/files", nil, nil, &body, &result)
	if err != nil {
		return err
	}

	if len(result) > 1 {
		panic(fmt.Sprintf("expected at most 1 result from API, got %d", len(result)))
	}

	if len(result) > 0 && result[0].Error != nil {
		return errors.New(result[0].Error.Message)
	}

	return nil
}

func convertOptionsToPayload(opts *MakeDirOptions) (*makeDirPayload, error) {
	user := opts.User
	group := opts.Group

	// UserID/GroupID take precedence over User/Group
	// If we don't do this, the call to MakeDir can fail if the user/group
	// name is not consistent with the ID, which can be counter-intuitive
	if opts.User != "" && opts.UserID != nil {
		user = ""
	}
	if opts.Group != "" && opts.GroupID != nil {
		group = ""
	}

	return &makeDirPayload{
		Action: "make-dirs",
		Dirs: []makeDirsItem{
			{
				Path:        opts.Path,
				MakeParents: opts.MakeParents,
				Permissions: opts.Permissions,
				UserID:      opts.UserID,
				User:        user,
				GroupID:     opts.GroupID,
				Group:       group,
			},
		},
	}, nil
}
