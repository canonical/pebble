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
	"io"
	"mime/multipart"
	"net/textproto"
	"strings"
)

// PushOptions contains the options for a call to Push.
type PushOptions struct {
	// Source is the source of data to write (required).
	Source io.Reader

	// Path indicates the absolute path of the file in the destination
	// machine (required).
	Path string

	// MakeDirs, if true, will create any non-existing directories in the path
	// to the remote file. If false, the default, the call to Push will
	// fail if any non-existing directory is found on the remote path.
	MakeDirs bool

	// Permissions indicates the mode of the file in the destination machine.
	// Defaults to 0644. Note that, when used together with MakeDirs, the
	// directories that might be created will not use this mode, but 0755.
	Permissions string

	// UserID indicates the user ID of the owner for the file in the destination
	// machine. When used together with MakeDirs, the directories that might be
	// created will also be owned by this user.
	UserID *int

	// User indicates the name of the owner user for the file in the destination
	// machine. When used together with MakeDirs, the directories that might be
	// created will also be owned by this user.
	User string

	// GroupID indicates the ID of the owner group for the file in the
	// destination machine. When used together with MakeDirs, the directories
	// that might be created will also be owned by this group.
	GroupID *int

	// Group indicates the name of the owner group for the file in the
	// destination machine. When used together with MakeDirs, the directories
	// that might be created will also be owned by this group.
	Group string
}

type writeFilesPayload struct {
	Action string           `json:"action"`
	Files  []writeFilesItem `json:"files"`
}

type writeFilesItem struct {
	Path        string `json:"path"`
	MakeDirs    bool   `json:"make-dirs"`
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

var quoteEscaper = strings.NewReplacer("\\", "\\\\", `"`, "\\\"")

func escapeQuotes(s string) string {
	return quoteEscaper.Replace(s)
}

// Push writes content to a path on the remote system.
func (client *Client) Push(opts *PushOptions) error {
	// Buffer for multipart header/footer
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)

	// Encode metadata part of the header
	part, err := mw.CreatePart(textproto.MIMEHeader{
		"Content-Type":        {"application/json"},
		"Content-Disposition": {`form-data; name="request"`},
	})
	if err != nil {
		return err
	}

	payload := writeFilesPayload{
		Action: "write",
		Files: []writeFilesItem{{
			Path:        opts.Path,
			MakeDirs:    opts.MakeDirs,
			Permissions: opts.Permissions,
			UserID:      opts.UserID,
			User:        opts.User,
			GroupID:     opts.GroupID,
			Group:       opts.Group,
		}},
	}
	if err = json.NewEncoder(part).Encode(&payload); err != nil {
		return err
	}

	// Encode file part of the header
	escapedPath := escapeQuotes(opts.Path)
	part, err = mw.CreatePart(textproto.MIMEHeader{
		"Content-Type":        {"application/octet-stream"},
		"Content-Disposition": {fmt.Sprintf(`form-data; name="files"; filename="%s"`, escapedPath)},
	})
	if err != nil {
		return err
	}

	header := b.String()

	// Encode multipart footer
	b.Reset()
	mw.Close()
	footer := b.String()

	body := io.MultiReader(strings.NewReader(header), opts.Source, strings.NewReader(footer))
	headers := map[string]string{
		"Content-Type": mw.FormDataContentType(),
	}

	var result []fileResult
	if _, err := client.doSync("POST", "/v1/files", nil, headers, body, &result); err != nil {
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
