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
	"net/http"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"time"
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

// PullOptions contains the options for a call to Pull.
type PullOptions struct {
	// Path indicates the absolute path of the file in the remote system
	// (required).
	Path string
}

// PullResult contains information about the result of a call to Pull.
type PullResult struct {
	// Reader is an io.ReadCloser for the file retrieved from the remote system.
	Reader io.ReadCloser
	// Size is the length in bytes of the file retrieved from the remote system.
	Size int64
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
	part, err = mw.CreatePart(textproto.MIMEHeader{
		"Content-Type":        {"application/octet-stream"},
		"Content-Disposition": {fmt.Sprintf(`form-data; name="files"; filename=%q`, opts.Path)},
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

	var result []fileResult
	_, err = client.doSync("POST", "/v1/files", nil, map[string]string{
		"Content-Type": mw.FormDataContentType(),
	}, body, &result)
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

// Pull retrieves a file from the remote system.
func (client *Client) Pull(opts *PullOptions) (*PullResult, error) {
	query := url.Values{
		"action": {"read"},
		"path":   {opts.Path},
	}
	headers := map[string]string{
		"Accept": "multipart/form-data",
	}

	retry := time.NewTicker(doRetry)
	defer retry.Stop()
	timeout := time.After(doTimeout)
	var rsp *http.Response
	var err error
	for {
		rsp, err = client.raw(context.Background(), "GET", "/v1/files", query, headers, nil)
		if err == nil {
			break
		}
		select {
		case <-retry.C:
			continue
		case <-timeout:
		}
		break
	}
	if err != nil {
		return nil, err
	}

	// Obtain Content-Type to check for a multipart payload and parse its value
	// in order to obtain the multipart boundary
	contentType := rsp.Header.Get("Content-Type")
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil, err
	}

	if mediaType != "multipart/form-data" {
		// Probably JSON-encoded error response
		var res response
		var fr []fileResult

		if err := decodeInto(rsp.Body, &res); err != nil {
			return nil, err
		}
		if err := res.err(client); err != nil {
			return nil, err
		}
		if res.Type != "sync" {
			return nil, fmt.Errorf("expected sync response, got %q", res.Type)
		}
		if err := decodeWithNumber(bytes.NewReader(res.Result), &fr); err != nil {
			return nil, fmt.Errorf("cannot unmarshal: %w", err)
		}

		if len(fr) != 1 {
			return nil, fmt.Errorf("expected exactly one result from API, got %d", len(fr))
		}

		if fr[0].Error != nil {
			return nil, &Error{
				Kind:    fr[0].Error.Kind,
				Value:   fr[0].Error.Value,
				Message: fr[0].Error.Message,
			}
		}

		// Not an error response after all
		return nil, fmt.Errorf("expected a multipart response but didn't get one")
	}

	// Obtain the file from the multipart payload
	mr := multipart.NewReader(rsp.Body, params["boundary"])
	part, err := mr.NextPart()
	if err != nil {
		return nil, err
	}
	// Obtain the file size from the Content-Length header
	size, err := strconv.ParseInt(part.Header.Get("Content-Length"), 10, 64)
	if err != nil {
		return nil, err
	}

	return &PullResult{
		Reader: part,
		Size:   size,
	}, nil
}
