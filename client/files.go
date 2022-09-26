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
)

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
