// Copyright (c) 2023 Canonical Ltd
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

package logstate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/canonical/pebble/internal/servicelog"
)

const (
	lokiClientTimeout = 10 * time.Second
)

type lokiClient struct {
	client *http.Client
	url    string
}

// Creates a new logClient for Loki.
// The provided URL is expected to be the fully qualified API path, e.g.
//
//	https://202.61.12.93:3100/loki/api/v1/push
func newLokiClient(url string) *lokiClient {
	return &lokiClient{
		client: &http.Client{
			Timeout: lokiClientTimeout,
		},
		url: url,
	}
}

func (c *lokiClient) Close() error {
	c.client.CloseIdleConnections()
	return nil
}

func (c *lokiClient) Send(entries []servicelog.Entry) error {
	if len(entries) == 0 {
		return nil // no-op
	}

	encodedEntries := make([]lokiMessage, 0, len(entries))
	for _, entry := range entries {
		encodedEntries = append(encodedEntries, encodeEntry(entry))
	}

	requestStruct := lokiRequest{
		Streams: []lokiStream{{
			Labels: map[string]string{
				// We need this label to distinguish where the logs came from.
				// Otherwise, we have no way to tell which service generated these logs.
				"pebble_service": entries[0].Service, // all entries should be from same service
				// TODO: allow specifying custom labels
			},
			Entries: encodedEntries,
		}},
	}

	data, err := json.Marshal(requestStruct)
	if err != nil {
		return err
	}

	body := bytes.NewReader(data)
	resp, err := c.client.Post(c.url, "application/json", body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Check response status code to see if it was successful
	if sc := resp.StatusCode; sc < 200 || sc >= 300 {
		// Request to Loki failed
		b, err := ioutil.ReadAll(io.LimitReader(resp.Body, 1024))
		if err != nil {
			b = append(b, []byte("//couldn't read response body: ")...)
			b = append(b, []byte(err.Error())...)
		}
		return fmt.Errorf("cannot send logs to Loki (HTTP %d), response body:\n%s", resp.StatusCode, b)
	}

	return nil
}

func encodeEntry(entry servicelog.Entry) lokiMessage {
	// Write in format
	//    ["<unix epoch in nanoseconds>","<log line>"]
	var encoded lokiMessage
	encoded[0] = strconv.FormatInt(entry.Time.UnixNano(), 10)
	encoded[1] = strings.TrimRight(entry.Message, "\n")
	return encoded
}

// Loki request types to encode to JSON
type lokiRequest struct {
	Streams []lokiStream `json:"streams"`
}

type lokiStream struct {
	Labels  map[string]string `json:"stream"`
	Entries []lokiMessage     `json:"values"`
}

type lokiMessage [2]string
