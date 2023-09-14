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

package clientutil

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/gorilla/websocket"
)

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

// Transport contains an HTTP client and information about it that can be used
// to create Client instances that are connection-detail agnostic.
type Transport struct {
	// Doer is an HTTP client instance to be used to perform requests.
	Doer Doer
	// BaseURL is needed to capture the scheme and hostname of the server.
	BaseURL url.URL
	// UserAgent will be passed alongside every request to identify the Client.
	UserAgent string

	client *http.Client
}

// NewTransport creates a new Transport instance with the specified connection
// details. When specifying the connection address, either a URL or a local
// Unix socket path can be specified.
func NewTransport(address string, userAgent string) (*Transport, error) {
	var httpTransport *http.Transport
	baseURL, err := url.Parse(address)
	if err != nil {
		return nil, fmt.Errorf("cannot parse base URL: %w", err)
	}

	if baseURL.Scheme == "" {
		// Talk over a Unix socket.
		baseURL = &url.URL{Scheme: "http", Host: "localhost"}
		httpTransport = &http.Transport{
			Dial: func(_, _ string) (net.Conn, error) {
				_, err := os.Stat(address)
				if errors.Is(err, os.ErrNotExist) {
					return nil, &SocketNotFoundError{Err: err, Path: address}
				} else if err != nil {
					return nil, fmt.Errorf("cannot stat %q: %w", address, err)
				}

				return net.Dial("unix", address)
			},
		}
	} else {
		// Talk regular HTTP over TCP.
		httpTransport = &http.Transport{}
	}

	client := &http.Client{Transport: httpTransport}
	return &Transport{
		Doer:      client,
		BaseURL:   *baseURL,
		UserAgent: userAgent,
		client:    client,
	}, nil
}

func (t *Transport) GetWebsocket(url string) (Websocket, error) {
	httpTransport := t.client.Transport.(*http.Transport)
	dialer := websocket.Dialer{
		NetDial:          httpTransport.Dial,
		Proxy:            httpTransport.Proxy,
		TLSClientConfig:  httpTransport.TLSClientConfig,
		HandshakeTimeout: 5 * time.Second,
	}
	conn, _, err := dialer.Dial(url, nil)
	return conn, err
}

func (t *Transport) CloseIdleConnections() {
	t.client.CloseIdleConnections()
}
