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
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"time"

	"github.com/gorilla/websocket"

	"github.com/canonical/pebble/internals/wsutil"
)

type Requester interface {
	// Do performs the HTTP transaction using the provided options.
	Do(ctx context.Context, opts *RequestOptions) (*RequestResponse, error)

	// Transport returns the HTTP transport in use by the underlying HTTP client.
	Transport() *http.Transport
}

type RequestType int

const (
	SyncRequest RequestType = iota
	AsyncRequest
	RawRequest
)

// RequestOptions allows setting up a specific request.
type RequestOptions struct {
	Type    RequestType
	Method  string
	Path    string
	Query   url.Values
	Headers map[string]string
	Body    io.Reader
}

// RequestResponse defines a common response associated with requests.
type RequestResponse struct {
	StatusCode int
	ChangeID   string
	Result     []byte

	// Only set for RawRequest and must be completely read and closed.
	Body io.ReadCloser
}

// DecodeResult can be used to decode a SyncRequest or AsyncRequest.
func (resp *RequestResponse) DecodeResult(result interface{}) error {
	if result != nil {
		if err := decodeWithNumber(bytes.NewReader(resp.Result), result); err != nil {
			return fmt.Errorf("cannot unmarshal: %w", err)
		}
	}

	return nil
}

// SocketNotFoundError is the error type returned when the client fails
// to find a unix socket at the specified path.
type SocketNotFoundError struct {
	// Err is the wrapped error.
	Err error

	// Path is the path of the non-existent socket.
	Path string
}

func (s SocketNotFoundError) Error() string {
	if s.Path == "" && s.Err != nil {
		return s.Err.Error()
	}
	return fmt.Sprintf("socket %q not found", s.Path)
}

func (s SocketNotFoundError) Unwrap() error {
	return s.Err
}

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
		_, err := os.Stat(socketPath)
		if errors.Is(err, os.ErrNotExist) {
			return nil, &SocketNotFoundError{Err: err, Path: socketPath}
		}
		if err != nil {
			return nil, fmt.Errorf("cannot stat %q: %w", socketPath, err)
		}

		return net.Dial("unix", socketPath)
	}
}

type doer interface {
	Do(*http.Request) (*http.Response, error)
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
	requester Requester

	maintenance      error
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

func New(config *Config) (*Client, error) {
	if config == nil {
		config = &Config{}
	}

	client := &Client{}
	requester, err := NewDefaultRequester(client, &DefaultRequesterConfig{
		Socket:           config.Socket,
		BaseURL:          config.BaseURL,
		DisableKeepAlive: config.DisableKeepAlive,
		UserAgent:        config.UserAgent,
	})
	if err != nil {
		return nil, err
	}

	client.requester = requester
	client.getWebsocket = func(url string) (clientWebsocket, error) {
		return getWebsocket(requester.Transport(), url)
	}

	return client, nil
}

func (client *Client) Requester() Requester {
	return client.requester
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

// CloseIdleConnections closes any API connections that are currently unused.
func (client *Client) CloseIdleConnections() {
	client.Requester().Transport().CloseIdleConnections()
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

// RequestError is returned when there's an error processing the request.
type RequestError struct{ error }

func (e RequestError) Error() string {
	return fmt.Sprintf("cannot build request: %v", e.error)
}

// ConnectionError represents a connection or communication error.
type ConnectionError struct {
	error
}

func (e ConnectionError) Error() string {
	return fmt.Sprintf("cannot communicate with server: %v", e.error)
}

func (e ConnectionError) Unwrap() error {
	return e.error
}

// raw creates an HTTP request, performs the request and returns an HTTP response
// and error.
func (br *DefaultRequester) raw(ctx context.Context, method, urlpath string, query url.Values, headers map[string]string, body io.Reader) (*http.Response, error) {
	// fake a url to keep http.Client happy
	u := br.baseURL
	u.Path = path.Join(br.baseURL.Path, urlpath)
	u.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, RequestError{err}
	}
	if br.userAgent != "" {
		req.Header.Set("User-Agent", br.userAgent)
	}

	for key, value := range headers {
		req.Header.Set(key, value)
	}

	rsp, err := br.doer.Do(req)
	if err != nil {
		return nil, ConnectionError{err}
	}

	return rsp, nil
}

var (
	rawRetry   = 250 * time.Millisecond
	rawTimeout = 5 * time.Second
)

// FakeDoRetry fakes the delays used by the do retry loop (intended for
// testing). Calling restore will revert the changes.
func FakeDoRetry(retry, timeout time.Duration) (restore func()) {
	oldRetry := rawRetry
	oldTimeout := rawTimeout
	rawRetry = retry
	rawTimeout = timeout
	return func() {
		rawRetry = oldRetry
		rawTimeout = oldTimeout
	}
}

// rawWithRetry builds in a retry mechanism for GET failures (body-less request)
func (br *DefaultRequester) rawWithRetry(ctx context.Context, method, urlpath string, query url.Values, headers map[string]string, body io.Reader) (*http.Response, error) {
	retry := time.NewTicker(rawRetry)
	defer retry.Stop()
	timeout := time.After(rawTimeout)
	var rsp *http.Response
	var err error
	for {
		rsp, err = br.raw(ctx, method, urlpath, query, headers, body)
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
		return nil, err
	}
	return rsp, nil
}

// Do implements all the required functionality as defined by the Requester interface. RequestOptions
// selects the Do behaviour. In case of a successful response, the result argument get a
// request specific result. The RequestResponse struct will include common attributes
// (not all will be set for all request types).
func (br *DefaultRequester) Do(ctx context.Context, opts *RequestOptions) (*RequestResponse, error) {
	httpResp, err := br.rawWithRetry(ctx, opts.Method, opts.Path, opts.Query, opts.Headers, opts.Body)
	if err != nil {
		return nil, err
	}

	// Is the result expecting a caller-managed raw body?
	if opts.Type == RawRequest {
		return &RequestResponse{
			Body: httpResp.Body,
		}, nil
	}

	// If we get here, this is a normal sync or async server request so
	// we have to close the body.
	defer httpResp.Body.Close()

	var serverResp response
	if err := decodeInto(httpResp.Body, &serverResp); err != nil {
		return nil, err
	}

	// Update the maintenance error state
	if serverResp.Maintenance != nil {
		br.client.maintenance = serverResp.Maintenance
	} else {
		br.client.maintenance = nil
	}

	// Deal with error type response
	if err := serverResp.err(); err != nil {
		return nil, err
	}

	// At this point only sync and async type requests may exist so lets
	// make sure this is the case.
	if opts.Type == SyncRequest {
		if serverResp.Type != "sync" {
			return nil, fmt.Errorf("expected sync response, got %q", serverResp.Type)
		}
	} else if opts.Type == AsyncRequest {
		if serverResp.Type != "async" {
			return nil, fmt.Errorf("expected async response for %q on %q, got %q", opts.Method, opts.Path, serverResp.Type)
		}
		if serverResp.StatusCode != http.StatusAccepted {
			return nil, fmt.Errorf("operation not accepted")
		}
		if serverResp.Change == "" {
			return nil, fmt.Errorf("async response without change reference")
		}
	} else {
		return nil, fmt.Errorf("cannot process unknown request type")
	}

	// Warnings are only included if not an error type response
	br.client.warningCount = serverResp.WarningCount
	br.client.warningTimestamp = serverResp.WarningTimestamp

	// Common response
	return &RequestResponse{
		StatusCode: serverResp.StatusCode,
		ChangeID:   serverResp.Change,
		Result:     serverResp.Result,
	}, nil
}

func decodeInto(reader io.Reader, v interface{}) error {
	dec := json.NewDecoder(reader)
	if err := dec.Decode(v); err != nil {
		r := dec.Buffered()
		buf, err1 := ioutil.ReadAll(r)
		if err1 != nil {
			buf = []byte(fmt.Sprintf("error reading buffered response body: %s", err1))
		}
		return fmt.Errorf("cannot decode %q: %w", buf, err)
	}
	return nil
}

func (client *Client) doSync(method, path string, query url.Values, headers map[string]string, body io.Reader, v interface{}) (*RequestResponse, error) {
	resp, err := client.Requester().Do(context.Background(), &RequestOptions{
		Type:    SyncRequest,
		Method:  method,
		Path:    path,
		Query:   query,
		Headers: headers,
		Body:    body,
	})
	if err != nil {
		return nil, err
	}
	err = resp.DecodeResult(v)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (client *Client) doAsync(method, path string, query url.Values, headers map[string]string, body io.Reader, v interface{}) (*RequestResponse, error) {
	resp, err := client.Requester().Do(context.Background(), &RequestOptions{
		Type:    AsyncRequest,
		Method:  method,
		Path:    path,
		Query:   query,
		Headers: headers,
		Body:    body,
	})
	if err != nil {
		return nil, err
	}
	err = resp.DecodeResult(v)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

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

// err extract the error in case of an error type response
func (rsp *response) err() error {
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

	err := rsp.err()
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

type DefaultRequesterConfig struct {
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

type DefaultRequester struct {
	baseURL   url.URL
	doer      doer
	userAgent string
	transport *http.Transport
	client    *Client
}

func NewDefaultRequester(client *Client, opts *DefaultRequesterConfig) (*DefaultRequester, error) {
	if opts == nil {
		opts = &DefaultRequesterConfig{}
	}

	var requester *DefaultRequester

	if opts.BaseURL == "" {
		// By default talk over a unix socket.
		transport := &http.Transport{Dial: unixDialer(opts.Socket), DisableKeepAlives: opts.DisableKeepAlive}
		baseURL := url.URL{Scheme: "http", Host: "localhost"}
		requester = &DefaultRequester{baseURL: baseURL, transport: transport}
	} else {
		// Otherwise talk regular HTTP-over-TCP.
		baseURL, err := url.Parse(opts.BaseURL)
		if err != nil {
			return nil, fmt.Errorf("cannot parse base URL: %v", err)
		}
		transport := &http.Transport{DisableKeepAlives: opts.DisableKeepAlive}
		requester = &DefaultRequester{baseURL: *baseURL, transport: transport}
	}

	requester.doer = &http.Client{Transport: requester.transport}
	requester.userAgent = opts.UserAgent
	requester.client = client

	return requester, nil
}

func (br *DefaultRequester) Transport() *http.Transport {
	return br.transport
}
