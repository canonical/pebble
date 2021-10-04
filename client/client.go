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
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"path"
	"time"

	"github.com/gorilla/websocket"

	"github.com/canonical/pebble/internal/wsutil"
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

func unixDialer(socketPath string) func(string, string) (net.Conn, error) {
	return func(_, _ string) (net.Conn, error) {
		return net.Dial("unix", socketPath)
	}
}

type doer interface {
	Do(*http.Request) (*http.Response, error)
}

// Config allows the user to customize client behavior.
type Config struct {
	// BaseURL contains the base URL where the pebble daemon is expected to be.
	// It can be empty for a default behavior of talking over a unix socket.
	BaseURL string

	// Socket is the path to the unix socket to use
	Socket string

	// DisableKeepAlive indicates whether the connections should not be kept
	// alive for later reuse
	DisableKeepAlive bool

	// User-Agent to sent to the pebble daemon
	UserAgent string
}

// A Client knows how to talk to the pebble daemon.
type Client struct {
	baseURL   url.URL
	doer      doer
	userAgent string

	maintenance error

	warningCount     int
	warningTimestamp time.Time

	getWebsocket getWebsocketFunc
}

type getWebsocketFunc func(url string) (clientWebsocket, error)

type clientWebsocket interface {
	wsutil.MessageReader
	wsutil.MessageWriter
	io.Closer
	jsonWriter
}

type jsonWriter interface {
	WriteJSON(v interface{}) error
}

// New returns a new instance of Client
func New(config *Config) (*Client, error) {
	if config == nil {
		config = &Config{}
	}

	var client *Client
	var transport *http.Transport

	if config.BaseURL == "" {
		// By default talk over a UNIX socket.
		transport = &http.Transport{Dial: unixDialer(config.Socket), DisableKeepAlives: config.DisableKeepAlive}
		baseURL := url.URL{Scheme: "http", Host: "localhost"}
		client = &Client{baseURL: baseURL}
	} else {
		// Otherwise talk regular HTTP-over-TCP.
		baseURL, err := url.Parse(config.BaseURL)
		if err != nil {
			return nil, fmt.Errorf("cannot parse base URL: %v", err)
		}
		transport = &http.Transport{DisableKeepAlives: config.DisableKeepAlive}
		client = &Client{baseURL: *baseURL}
	}

	client.doer = &http.Client{Transport: transport}
	client.userAgent = config.UserAgent
	client.getWebsocket = func(url string) (clientWebsocket, error) {
		return getWebsocket(transport, url)
	}

	return client, nil
}

func (client *Client) getTaskWebsocket(taskID, websocketID string) (clientWebsocket, error) {
	url := fmt.Sprintf("ws://localhost/v1/tasks/%s/websocket/%s", taskID, websocketID)
	return client.getWebsocket(url)
}

func getWebsocket(transport *http.Transport, url string) (clientWebsocket, error) {
	dialer := websocket.Dialer{
		NetDial:          transport.Dial,
		Proxy:            transport.Proxy,
		TLSClientConfig:  transport.TLSClientConfig,
		HandshakeTimeout: 5 * time.Second,
	}
	conn, _, err := dialer.Dial(url, nil)
	return conn, err
}

func (client *Client) CloseIdleConnections() {
	c, ok := client.doer.(*http.Client)
	if ok {
		c.CloseIdleConnections()
	}
}

// Maintenance returns an error reflecting the daemon maintenance status or nil.
func (client *Client) Maintenance() error {
	return client.maintenance
}

// WarningsSummary returns the number of warnings that are ready to be shown to
// the user, and the timestamp of the most recently added warning (useful for
// silencing the warning alerts, and OKing the returned warnings).
func (client *Client) WarningsSummary() (count int, timestamp time.Time) {
	return client.warningCount, client.warningTimestamp
}

type RequestError struct{ error }

func (e RequestError) Error() string {
	return fmt.Sprintf("cannot build request: %v", e.error)
}

type ConnectionError struct{ error }

func (e ConnectionError) Error() string {
	return fmt.Sprintf("cannot communicate with server: %v", e.error)
}

// raw performs a request and returns the resulting http.Response and
// error you usually only need to call this directly if you expect the
// response to not be JSON, otherwise you'd call Do(...) instead.
func (client *Client) raw(ctx context.Context, method, urlpath string, query url.Values, headers map[string]string, body io.Reader) (*http.Response, error) {
	// fake a url to keep http.Client happy
	u := client.baseURL
	u.Path = path.Join(client.baseURL.Path, urlpath)
	u.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, RequestError{err}
	}
	if client.userAgent != "" {
		req.Header.Set("User-Agent", client.userAgent)
	}

	for key, value := range headers {
		req.Header.Set(key, value)
	}

	rsp, err := client.doer.Do(req)
	if err != nil {
		return nil, ConnectionError{err}
	}

	return rsp, nil
}

var (
	doRetry   = 250 * time.Millisecond
	doTimeout = 5 * time.Second
)

// FakeDoRetry fakes the delays used by the do retry loop.
func FakeDoRetry(retry, timeout time.Duration) (restore func()) {
	oldRetry := doRetry
	oldTimeout := doTimeout
	doRetry = retry
	doTimeout = timeout
	return func() {
		doRetry = oldRetry
		doTimeout = oldTimeout
	}
}

type hijacked struct {
	do func(*http.Request) (*http.Response, error)
}

func (h hijacked) Do(req *http.Request) (*http.Response, error) {
	return h.do(req)
}

// Hijack lets the caller take over the raw http request
func (client *Client) Hijack(f func(*http.Request) (*http.Response, error)) {
	client.doer = hijacked{f}
}

// do performs a request and decodes the resulting json into the given
// value. It's low-level, for testing/experimenting only; you should
// usually use a higher level interface that builds on this.
func (client *Client) do(method, path string, query url.Values, headers map[string]string, body io.Reader, v interface{}) error {
	retry := time.NewTicker(doRetry)
	defer retry.Stop()
	timeout := time.After(doTimeout)
	var rsp *http.Response
	var err error
	for {
		rsp, err = client.raw(context.Background(), method, path, query, headers, body)
		if err == nil || method != "GET" {
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
		return err
	}
	defer rsp.Body.Close()

	if v != nil {
		if err := decodeInto(rsp.Body, v); err != nil {
			return err
		}
	}

	return nil
}

func decodeInto(reader io.Reader, v interface{}) error {
	dec := json.NewDecoder(reader)
	if err := dec.Decode(v); err != nil {
		r := dec.Buffered()
		buf, err1 := ioutil.ReadAll(r)
		if err1 != nil {
			buf = []byte(fmt.Sprintf("error reading buffered response body: %s", err1))
		}
		return fmt.Errorf("cannot decode %q: %s", buf, err)
	}
	return nil
}

// doSync performs a request to the given path using the specified HTTP method.
// It expects a "sync" response from the API and on success decodes the JSON
// response payload into the given value using the "UseNumber" json decoding
// which produces json.Numbers instead of float64 types for numbers.
func (client *Client) doSync(method, path string, query url.Values, headers map[string]string, body io.Reader, v interface{}) (*ResultInfo, error) {
	var rsp response
	if err := client.do(method, path, query, headers, body, &rsp); err != nil {
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
			return nil, fmt.Errorf("cannot unmarshal: %v", err)
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

	if err := client.do(method, path, query, headers, body, &rsp); err != nil {
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
		return fmt.Errorf("cannot unmarshal error: %v", err)
	}

	err := rsp.err(nil)
	if err == nil {
		return fmt.Errorf("server error: %q", r.Status)
	}
	return err
}

type SysInfo struct {
	Version string `json:"version,omitempty"`
	BootID  string `json:"boot-id,omitempty"`
}

// SysInfo gets system information from the remote API.
func (client *Client) SysInfo() (*SysInfo, error) {
	var sysInfo SysInfo

	if _, err := client.doSync("GET", "/v1/system-info", nil, nil, nil, &sysInfo); err != nil {
		return nil, fmt.Errorf("cannot obtain system details: %v", err)
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
