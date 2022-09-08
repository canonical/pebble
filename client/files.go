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

	// Dest is the destination io.Writer that will receive the data (required).
	Dest io.Writer
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
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("cannot decode multipart payload: %w", err)
		}
		defer part.Close()

		if part.FormName() == "files" {
			if _, err = io.Copy(opts.Dest, part); err != nil {
				return fmt.Errorf("cannot write: %w", err)
			}
		} else if part.FormName() == "response" {
			// Process response metadata
			var res response
			var fr []fileResult

			decoder := json.NewDecoder(part)
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
		}
	}

	return nil
}
