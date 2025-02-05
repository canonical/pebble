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
	RawRequest RequestType = iota
	SyncRequest
	AsyncRequest
)

type RequestOptions struct {
	Type    RequestType
	Method  string
	Path    string
	Query   url.Values
	Headers map[string]string
	Body    io.Reader
}

type RequestResponse struct {
	StatusCode int
	Headers    http.Header
	// ChangeID is typically set when an AsyncRequest type is performed. The
	// change id allows for introspection and progress tracking of the request.
	ChangeID string
	// Result can contain request specific JSON data. The result can be
	// unmarshalled into the expected type using the DecodeResult method.
	Result []byte
	// Body is only set for request type RawRequest.
	Body io.ReadCloser
}

// DecodeResult decodes the endpoint-specific result payload that is included as part of
// sync and async request responses. The decoding is performed with the standard JSON
// package, so the usual field tags should be used to prepare the type for decoding.
func (resp *RequestResponse) DecodeResult(result any) error {
	reader := bytes.NewReader(resp.Result)
	dec := json.NewDecoder(reader)
	dec.UseNumber()
	if err := dec.Decode(&result); err != nil {
		return fmt.Errorf("cannot unmarshal: %w", err)
	}
	if dec.More() {
		return fmt.Errorf("cannot unmarshal: cannot parse json value")
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

	maintenance   error
	latestWarning time.Time

	getWebsocket getWebsocketFunc

	host string
}

type getWebsocketFunc func(url string) (clientWebsocket, error)

type clientWebsocket interface {
	wsutil.MessageReader
	wsutil.MessageWriter
	io.Closer
	jsonWriter
}

type jsonWriter interface {
	WriteJSON(v any) error
}

func New(config *Config) (*Client, error) {
	if config == nil {
		config = &Config{}
	}

	client := &Client{}
	requester, err := newDefaultRequester(client, config)
	if err != nil {
		return nil, err
	}

	client.requester = requester
	client.getWebsocket = func(url string) (clientWebsocket, error) {
		return getWebsocket(requester.Transport(), url)
	}
	client.host = requester.baseURL.Host

	return client, nil
}

func (client *Client) Requester() Requester {
	return client.requester
}

func (client *Client) getTaskWebsocket(taskID, websocketID string) (clientWebsocket, error) {
	url := fmt.Sprintf("ws://%s/v1/tasks/%s/websocket/%s", client.host, taskID, websocketID)
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

// LatestWarningTime returns the most recent time a warning notice was
// repeated, or the zero value if there are no warnings.
func (client *Client) LatestWarningTime() time.Time {
	return client.latestWarning
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

func (rq *defaultRequester) dispatch(ctx context.Context, method, urlpath string, query url.Values, headers map[string]string, body io.Reader) (*http.Response, error) {
	// fake a url to keep http.Client happy
	u := rq.baseURL
	u.Path = path.Join(rq.baseURL.Path, urlpath)
	u.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, RequestError{err}
	}
	if rq.userAgent != "" {
		req.Header.Set("User-Agent", rq.userAgent)
	}

	for key, value := range headers {
		req.Header.Set(key, value)
	}

	rsp, err := rq.doer.Do(req)
	if err != nil {
		return nil, ConnectionError{err}
	}

	return rsp, nil
}

var (
	doRetry   = 250 * time.Millisecond
	doTimeout = 5 * time.Second
)

// FakeDoRetry fakes the delays used by the do retry loop (intended for
// testing). Calling restore will revert the changes.
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

// retry builds in a retry mechanism for GET failures.
func (rq *defaultRequester) retry(ctx context.Context, method, urlpath string, query url.Values, headers map[string]string, body io.Reader) (*http.Response, error) {
	retry := time.NewTicker(doRetry)
	defer retry.Stop()
	timeout := time.After(doTimeout)
	var rsp *http.Response
	var err error
	for {
		rsp, err = rq.dispatch(ctx, method, urlpath, query, headers, body)
		if err == nil || method != "GET" {
			break
		}
		select {
		case <-retry.C:
			continue
		case <-timeout:
		case <-ctx.Done():
		}
		break
	}
	if err != nil {
		return nil, err
	}
	return rsp, nil
}

// Do performs the HTTP request according to the provided options, possibly retrying GET requests
// if appropriate for the status reported by the server.
func (rq *defaultRequester) Do(ctx context.Context, opts *RequestOptions) (*RequestResponse, error) {
	httpResp, err := rq.retry(ctx, opts.Method, opts.Path, opts.Query, opts.Headers, opts.Body)
	if err != nil {
		return nil, err
	}

	// Is the result expecting a caller-managed raw body?
	if opts.Type == RawRequest {
		return &RequestResponse{
			StatusCode: httpResp.StatusCode,
			Headers:    httpResp.Header,
			Body:       httpResp.Body,
		}, nil
	}

	defer httpResp.Body.Close()
	var serverResp response
	if err := decodeInto(httpResp.Body, &serverResp); err != nil {
		return nil, err
	}

	// Update the maintenance error state
	if serverResp.Maintenance != nil {
		rq.client.maintenance = serverResp.Maintenance
	} else {
		// We cannot assign a nil pointer of type *Error to an
		// interface here because the interface is only nil if
		// both the type and value is nil.
		// https://go.dev/doc/faq#nil_error
		rq.client.maintenance = nil
	}

	// Deal with error type response
	if err := serverResp.err(); err != nil {
		return nil, err
	}

	// At this point only sync and async type requests may exist so lets
	// make sure this is the case.
	switch opts.Type {
	case SyncRequest:
		if serverResp.Type != "sync" {
			return nil, fmt.Errorf("expected sync response, got %q", serverResp.Type)
		}
	case AsyncRequest:
		if serverResp.Type != "async" {
			return nil, fmt.Errorf("expected async response for %q on %q, got %q", opts.Method, opts.Path, serverResp.Type)
		}
		if serverResp.StatusCode != http.StatusAccepted {
			return nil, fmt.Errorf("operation not accepted")
		}
		if serverResp.Change == "" {
			return nil, fmt.Errorf("async response without change reference")
		}
	default:
		return nil, fmt.Errorf("cannot process unknown request type")
	}

	// Warnings are only included if not an error type response, so we don't
	// replace valid local warnings with an empty state that comes from a failure.
	rq.client.latestWarning = serverResp.LatestWarning

	// Common response
	return &RequestResponse{
		StatusCode: serverResp.StatusCode,
		Headers:    httpResp.Header,
		ChangeID:   serverResp.Change,
		Result:     serverResp.Result,
	}, nil
}

func decodeInto(reader io.Reader, v any) error {
	dec := json.NewDecoder(reader)
	if err := dec.Decode(v); err != nil {
		r := dec.Buffered()
		buf, err1 := io.ReadAll(r)
		if err1 != nil {
			buf = []byte(fmt.Sprintf("error reading buffered response body: %s", err1))
		}
		return fmt.Errorf("cannot decode %q: %w", buf, err)
	}
	return nil
}

// A response produced by the REST API will usually fit in this
// (exceptions are the icons/ endpoints obvs)
type response struct {
	Result        json.RawMessage `json:"result"`
	Status        string          `json:"status"`
	StatusCode    int             `json:"status-code"`
	Type          string          `json:"type"`
	Change        string          `json:"change"`
	LatestWarning time.Time       `json:"latest-warning"`
	Maintenance   *Error          `json:"maintenance"`
}

// Error is the real value of response.Result when an error occurs.
type Error struct {
	Kind    string `json:"kind"`
	Value   any    `json:"value"`
	Message string `json:"message"`

	StatusCode int
}

func (e *Error) Error() string {
	return e.Message
}

// Error kinds for use as a response or maintenance result
const (
	ErrorKindLoginRequired     = "login-required"
	ErrorKindNoDefaultServices = "no-default-services"
	ErrorKindNotFound          = "not-found"
	ErrorKindPermissionDenied  = "permission-denied"
	ErrorKindGenericFileError  = "generic-file-error"
	ErrorKindSystemRestart     = "system-restart"
	ErrorKindDaemonRestart     = "daemon-restart"
)

// err extracts the error in case of an error type response
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

	resp, err := client.Requester().Do(context.Background(), &RequestOptions{
		Type:   SyncRequest,
		Method: "GET",
		Path:   "/v1/system-info",
	})
	if err != nil {
		return nil, fmt.Errorf("cannot obtain system details: %w", err)
	}
	err = resp.DecodeResult(&sysInfo)
	if err != nil {
		return nil, fmt.Errorf("cannot obtain system details: %w", err)
	}

	return &sysInfo, nil
}

type debugAction struct {
	Action string `json:"action"`
	Params any    `json:"params,omitempty"`
}

// DebugPost sends a POST debug action to the server with the provided parameters.
func (client *Client) DebugPost(action string, params any, result any) error {
	body, err := json.Marshal(debugAction{
		Action: action,
		Params: params,
	})
	if err != nil {
		return err
	}

	resp, err := client.Requester().Do(context.Background(), &RequestOptions{
		Type:   SyncRequest,
		Method: "POST",
		Path:   "/v1/debug",
		Body:   bytes.NewReader(body),
	})
	if err != nil {
		return err
	}
	err = resp.DecodeResult(result)
	if err != nil {
		return err
	}
	return err
}

// DebugGet sends a GET debug action to the server with the provided parameters.
func (client *Client) DebugGet(action string, result any, params map[string]string) error {
	urlParams := url.Values{"action": []string{action}}
	for k, v := range params {
		urlParams.Set(k, v)
	}
	resp, err := client.Requester().Do(context.Background(), &RequestOptions{
		Type:   SyncRequest,
		Method: "GET",
		Path:   "/v1/debug",
		Query:  urlParams,
	})
	if err != nil {
		return err
	}
	return resp.DecodeResult(&result)
}

type defaultRequester struct {
	baseURL   url.URL
	doer      doer
	userAgent string
	transport *http.Transport
	client    *Client
}

func newDefaultRequester(client *Client, opts *Config) (*defaultRequester, error) {
	if opts == nil {
		opts = &Config{}
	}

	var requester *defaultRequester

	if opts.BaseURL == "" {
		// By default talk over a unix socket.
		transport := &http.Transport{Dial: unixDialer(opts.Socket), DisableKeepAlives: opts.DisableKeepAlive}
		baseURL := url.URL{Scheme: "http", Host: "localhost"}
		requester = &defaultRequester{baseURL: baseURL, transport: transport}
	} else {
		// Otherwise talk regular HTTP-over-TCP.
		baseURL, err := url.Parse(opts.BaseURL)
		if err != nil {
			return nil, fmt.Errorf("cannot parse base URL: %w", err)
		}
		transport := &http.Transport{DisableKeepAlives: opts.DisableKeepAlive}
		requester = &defaultRequester{baseURL: *baseURL, transport: transport}
	}

	requester.doer = &http.Client{Transport: requester.transport}
	requester.userAgent = opts.UserAgent
	requester.client = client

	return requester, nil
}

func (rq *defaultRequester) Transport() *http.Transport {
	return rq.transport
}
