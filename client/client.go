// Copyright (c) 2014-2020 Canonical Ltd
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
	"net/http"
	"net/url"
	"time"

	"github.com/canonical/pebble/internals/clientutil"
)

// decodeWithNumber decodes input data using json.Decoder, ensuring numbers are preserved
// via json.Number data type. It errors out on invalid json or any excess input.
func decodeWithNumber(r io.Reader, value interface{}) error {
	dec := json.NewDecoder(r)
	dec.UseNumber()
	if err := dec.Decode(&value); err != nil {
		return err
	}
	if dec.More() {
		return fmt.Errorf("cannot parse json value")
	}
	return nil
}

// Config allows the user to customize client behavior.
type Config struct {
	// BaseURL contains the base URL where the Pebble daemon is expected to be.
	// It can be empty for a default behavior of talking over a unix socket.
	BaseURL string

	// Socket is the path to the unix socket to use.
	Socket string

	// DisableKeepAlive indicates that the connections should not be kept
	// alive for later reuse (the default is to keep them alive).
	DisableKeepAlive bool

	// UserAgent is the User-Agent header sent to the Pebble daemon.
	UserAgent string
}

// A Client knows how to talk to the Pebble daemon.
type Client struct {
	transport *clientutil.Transport

	maintenance error

	warningCount     int
	warningTimestamp time.Time

	getWebsocket getWebsocketFunc
}

type getWebsocketFunc func(url string) (clientutil.Websocket, error)

func New(config *Config) (client *Client, err error) {
	if config == nil {
		config = &Config{}
	}

	var transport *clientutil.Transport
	if config.BaseURL == "" {
		// A Unix socket was specified.
		transport, err = clientutil.NewTransport(config.Socket, config.UserAgent)
	} else {
		// A URL was specified.
		transport, err = clientutil.NewTransport(config.BaseURL, config.UserAgent)
	}
	if err != nil {
		return
	}

	return &Client{
		transport: transport,
		getWebsocket: func(url string) (clientutil.Websocket, error) {
			return transport.GetWebsocket(url)
		},
	}, nil
}

func NewFromTransport(transport *clientutil.Transport) *Client {
	return &Client{transport: transport}
}

func (c *Client) getTaskWebsocket(taskID, websocketID string) (clientutil.Websocket, error) {
	url := fmt.Sprintf("ws://localhost/v1/tasks/%s/websocket/%s", taskID, websocketID)
	return c.getWebsocket(url)
}

// CloseIdleConnections closes any API connections that are currently unused.
func (c *Client) CloseIdleConnections() {
	c.transport.CloseIdleConnections()
}

// Maintenance returns an error reflecting the daemon maintenance status or nil.
func (c *Client) Maintenance() error {
	return c.maintenance
}

// WarningsSummary returns the number of warnings that are ready to be shown to
// the user, and the timestamp of the most recently added warning (useful for
// silencing the warning alerts, and OKing the returned warnings).
func (c *Client) WarningsSummary() (count int, timestamp time.Time) {
	return c.warningCount, c.warningTimestamp
}

// doSync performs a request to the given path using the specified HTTP method.
// It expects a "sync" response from the API and on success decodes the JSON
// response payload into the given value using the "UseNumber" json decoding
// which produces json.Numbers instead of float64 types for numbers.
func (client *Client) doSync(method, path string, query url.Values, headers map[string]string, body io.Reader, v interface{}) (*ResultInfo, error) {
	var rsp response
	if err := client.transport.Do(method, path, query, headers, body, &rsp); err != nil {
		return nil, err
	}
	if err := rsp.err(client); err != nil {
		return nil, err
	}
	if rsp.Type != "sync" {
		return nil, fmt.Errorf("expected sync response, got %q", rsp.Type)
	}

	if v != nil {
		if err := decodeWithNumber(bytes.NewReader(rsp.Result), v); err != nil {
			return nil, fmt.Errorf("cannot unmarshal: %w", err)
		}
	}

	client.warningCount = rsp.WarningCount
	client.warningTimestamp = rsp.WarningTimestamp

	return &rsp.ResultInfo, nil
}

func (client *Client) doAsync(method, path string, query url.Values, headers map[string]string, body io.Reader) (changeID string, err error) {
	_, changeID, err = client.doAsyncFull(method, path, query, headers, body)
	return
}

func (client *Client) doAsyncFull(method, path string, query url.Values, headers map[string]string, body io.Reader) (result json.RawMessage, changeID string, err error) {
	var rsp response

	if err := client.transport.Do(method, path, query, headers, body, &rsp); err != nil {
		return nil, "", err
	}
	if err := rsp.err(client); err != nil {
		return nil, "", err
	}
	if rsp.Type != "async" {
		return nil, "", fmt.Errorf("expected async response for %q on %q, got %q", method, path, rsp.Type)
	}
	if rsp.StatusCode != 202 {
		return nil, "", fmt.Errorf("operation not accepted")
	}
	if rsp.Change == "" {
		return nil, "", fmt.Errorf("async response without change reference")
	}

	return rsp.Result, rsp.Change, nil
}

// ResultInfo is empty for now, but this is the mechanism that conveys
// general information that makes sense to requests at a more general
// level, and might be disconnected from the specific request at hand.
type ResultInfo struct{}

// A response produced by the REST API will usually fit in this
// (exceptions are the icons/ endpoints obvs)
type response struct {
	Result     json.RawMessage `json:"result"`
	Status     string          `json:"status"`
	StatusCode int             `json:"status-code"`
	Type       string          `json:"type"`
	Change     string          `json:"change"`

	WarningCount     int       `json:"warning-count"`
	WarningTimestamp time.Time `json:"warning-timestamp"`

	ResultInfo

	Maintenance *Error `json:"maintenance"`
}

// Error is the real value of response.Result when an error occurs.
type Error struct {
	Kind    string      `json:"kind"`
	Value   interface{} `json:"value"`
	Message string      `json:"message"`

	StatusCode int
}

func (e *Error) Error() string {
	return e.Message
}

const (
	ErrorKindLoginRequired     = "login-required"
	ErrorKindSystemRestart     = "system-restart"
	ErrorKindDaemonRestart     = "daemon-restart"
	ErrorKindNoDefaultServices = "no-default-services"
)

func (rsp *response) err(cli *Client) error {
	if cli != nil {
		maintErr := rsp.Maintenance
		// avoid setting to (*client.Error)(nil)
		if maintErr != nil {
			cli.maintenance = maintErr
		} else {
			cli.maintenance = nil
		}
	}
	if rsp.Type != "error" {
		return nil
	}
	var resultErr Error
	err := json.Unmarshal(rsp.Result, &resultErr)
	if err != nil || resultErr.Message == "" {
		return fmt.Errorf("server error: %q", rsp.Status)
	}
	resultErr.StatusCode = rsp.StatusCode

	return &resultErr
}

func parseError(r *http.Response) error {
	var rsp response
	if r.Header.Get("Content-Type") != "application/json" {
		return fmt.Errorf("server error: %q", r.Status)
	}

	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&rsp); err != nil {
		return fmt.Errorf("cannot unmarshal error: %w", err)
	}

	err := rsp.err(nil)
	if err == nil {
		return fmt.Errorf("server error: %q", r.Status)
	}
	return err
}

type SysInfo struct {
	// Version is the server version.
	Version string `json:"version,omitempty"`

	// BootID is a unique string that represents this boot of the server.
	BootID string `json:"boot-id,omitempty"`
}

// SysInfo gets system information from the remote API.
func (client *Client) SysInfo() (*SysInfo, error) {
	var sysInfo SysInfo

	if _, err := client.doSync("GET", "/v1/system-info", nil, nil, nil, &sysInfo); err != nil {
		return nil, fmt.Errorf("cannot obtain system details: %w", err)
	}

	return &sysInfo, nil
}

type debugAction struct {
	Action string      `json:"action"`
	Params interface{} `json:"params,omitempty"`
}

// DebugPost sends a POST debug action to the server with the provided parameters.
func (client *Client) DebugPost(action string, params interface{}, result interface{}) error {
	body, err := json.Marshal(debugAction{
		Action: action,
		Params: params,
	})
	if err != nil {
		return err
	}

	_, err = client.doSync("POST", "/v1/debug", nil, nil, bytes.NewReader(body), result)
	return err
}

// DebugGet sends a GET debug action to the server with the provided parameters.
func (client *Client) DebugGet(action string, result interface{}, params map[string]string) error {
	urlParams := url.Values{"action": []string{action}}
	for k, v := range params {
		urlParams.Set(k, v)
	}
	_, err := client.doSync("GET", "/v1/debug", urlParams, nil, nil, &result)
	return err
}
