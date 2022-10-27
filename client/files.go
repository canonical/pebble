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
)

// PullOptions contains the options for a call to Pull.
type PullOptions struct {
	// Path indicates the absolute path of the file in the remote system
	// (required).
	Path string

	// Target is the destination io.Writer that will receive the data (required).
	Target io.Writer
}

type fileResult struct {
	Path  string `json:"path"`
	Error *Error `json:"error,omitempty"`
}

// Pull retrieves a file from the remote system.
func (client *Client) Pull(opts *PullOptions) error {
	query := url.Values{
		"action": {"read"},
		"path":   {opts.Path},
	}
	headers := map[string]string{
		"Accept": "multipart/form-data",
	}

	rsp, err := client.raw(context.Background(), "GET", "/v1/files", query, headers, nil)
	if err != nil {
		return err
	}
	defer rsp.Body.Close()

	// Obtain Content-Type to check for a multipart payload and parse its value
	// in order to obtain the multipart boundary
	contentType := rsp.Header.Get("Content-Type")
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return fmt.Errorf("invalid Content-Type: %w", err)
	}
	if mediaType != "multipart/form-data" {
		// Not an error response after all
		return fmt.Errorf("expected a multipart response but didn't get one")
	}

	mr := multipart.NewReader(rsp.Body, params["boundary"])

	filesPart, err := mr.NextPart()
	if err != nil {
		return fmt.Errorf("cannot decode multipart payload: %w", err)
	}
	defer filesPart.Close()
	if filesPart.FormName() != "files" {
		return fmt.Errorf(`expected first field name to be "files", got %q`, filesPart.FormName())
	}
	if _, err = io.Copy(opts.Target, filesPart); err != nil {
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

	// Process response metadata
	var res response
	var fr []fileResult

	decoder := json.NewDecoder(responsePart)
	if err := decoder.Decode(&res); err != nil {
		return fmt.Errorf("cannot decode response: %w", err)
	}
	if err := res.err(client); err != nil {
		return err
	}
	if res.Type != "sync" {
		return fmt.Errorf("expected sync response, got %q", res.Type)
	}
	if err := decodeWithNumber(bytes.NewReader(res.Result), &fr); err != nil {
		return fmt.Errorf("cannot unmarshal result: %w", err)
	}

	if len(fr) != 1 {
		return fmt.Errorf("expected exactly one result from API, got %d", len(fr))
	}
	if fr[0].Error != nil {
		return &Error{
			Kind:    fr[0].Error.Kind,
			Value:   fr[0].Error.Value,
			Message: fr[0].Error.Message,
		}
	}

	return nil
}
