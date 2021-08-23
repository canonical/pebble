// Copyright (c) 2021 Canonical Ltd
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
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/gorilla/websocket"

	"github.com/canonical/pebble/internal/wsutil"
)

// ExecOptions are the main options for the Exec call.
type ExecOptions struct {
	// Required: command and arguments (first element is the executable)
	Command []string

	// Optional environment variables
	Environment map[string]string

	// Optional working directory (default is $HOME or "/" if $HOME not set).
	WorkingDir string

	// Optional timeout for the command execution, after which the process
	// will be terminated. If zero, no timeout applies.
	Timeout time.Duration

	// Optional user ID and group ID for the process to run as.
	UserID  *int
	User    string
	GroupID *int
	Group   string

	// True to ask the server to set up a pseudo-terminal (PTY) -- this allows
	// full interactivity and window resizing. The default is no PTY, and just
	// to use pipes for stdin/stdout/stderr.
	Terminal bool

	// True to separate the process's stderr into a separate websocket. The
	// default is to send combined stdout+stderr on a single websocket.
	Stderr bool

	// Initial terminal width and height (only apply if Terminal is true)
	Width  int
	Height int
}

// ExecAdditionalArgs are additional control arguments for the Exec call.
type ExecAdditionalArgs struct {
	// Standard input, output, and error.
	Stdin  io.ReadCloser
	Stdout io.WriteCloser
	Stderr io.WriteCloser

	// Control message handler (for window resizing and signal forwarding)
	Control func(conn *websocket.Conn)

	// Channel that will be closed when all data operations are done
	DataDone chan bool
}

// Exec starts a command execution with the given options and additional
// control arguments, returning the execution's change ID.
func (client *Client) Exec(opts *ExecOptions, args *ExecAdditionalArgs) (string, error) {
	if opts.Terminal && opts.Stderr {
		return "", errors.New("separate stderr not currently supported in terminal mode")
	}
	if !opts.Terminal && !opts.Stderr {
		return "", errors.New("combined stderr not currently supported in non-terminal mode")
	}

	var payload = struct {
		Command     []string          `json:"command"`
		Environment map[string]string `json:"environment"`
		WorkingDir  string            `json:"working-dir"`
		Timeout     time.Duration     `json:"timeout"`
		UserID      *int              `json:"user-id"`
		User        string            `json:"user"`
		GroupID     *int              `json:"group-id"`
		Group       string            `json:"group"`
		Terminal    bool              `json:"terminal"`
		Stderr      bool              `json:"stderr"`
		Width       int               `json:"width"`
		Height      int               `json:"height"`
	}(*opts)
	var body bytes.Buffer
	err := json.NewEncoder(&body).Encode(&payload)
	if err != nil {
		return "", err
	}
	headers := map[string]string{
		"Content-Type": "application/json",
	}
	resultBytes, changeID, err := client.doAsyncFull("POST", "/v1/exec", nil, headers, &body)
	if err != nil {
		return "", err
	}
	var result struct {
		WebsocketIDs map[string]string `json:"websocket-ids"`
	}
	err = json.Unmarshal(resultBytes, &result)
	if err != nil {
		return "", err
	}

	if args == nil {
		return changeID, nil
	}

	// Process additional arguments (connecting I/O and websockets)
	fds := result.WebsocketIDs

	// Call the control handler with a connection to the control socket
	if args.Control != nil && fds["control"] != "" {
		conn, err := client.getChangeWebsocket(changeID, fds["control"])
		if err != nil {
			return "", err
		}
		go args.Control(conn)
	}

	if opts.Terminal {
		// Handle interactive sessions
		if args.Stdin != nil && args.Stdout != nil {
			// Connect to the websocket
			conn, err := client.getChangeWebsocket(changeID, fds["io"])
			if err != nil {
				return "", err
			}

			// And attach stdin and stdout to it
			go func() {
				wsutil.WebsocketSendStream(conn, args.Stdin, -1)
				<-wsutil.WebsocketRecvStream(args.Stdout, conn)
				conn.Close()
				if args.DataDone != nil {
					close(args.DataDone)
				}
			}()
		} else {
			if args.DataDone != nil {
				close(args.DataDone)
			}
		}
	} else {
		// Handle non-interactive sessions
		dones := map[string]chan bool{}
		conns := []*websocket.Conn{}

		// Handle stdin and stdout
		if fds["io"] != "" {
			conn, err := client.getChangeWebsocket(changeID, fds["io"])
			if err != nil {
				return "", err
			}
			conns = append(conns, conn)
			dones["stdin"] = wsutil.WebsocketSendStream(conn, args.Stdin, -1)
			dones["stdout"] = wsutil.WebsocketRecvStream(args.Stdout, conn)
		}

		// Handle stderr separately if needed
		if opts.Stderr && fds["stderr"] != "" {
			conn, err := client.getChangeWebsocket(changeID, fds["stderr"])
			if err != nil {
				return "", err
			}
			conns = append(conns, conn)
			dones["stderr"] = wsutil.WebsocketRecvStream(args.Stderr, conn)
		}

		// Wait for everything to be done
		go func() {
			for name, done := range dones {
				// Skip stdin, dealing with it separately below
				if name == "stdin" {
					continue
				}
				<-done
			}

			if fds["io"] != "" {
				if args.Stdin != nil {
					args.Stdin.Close()
				}

				// Empty the stdin channel but don't block on it as
				// stdin may be stuck in Read()
				go func() {
					<-dones["stdin"]
				}()
			}

			for _, conn := range conns {
				conn.Close()
			}

			if args.DataDone != nil {
				close(args.DataDone)
			}
		}()
	}

	return changeID, nil
}

// getChangeWebsocket creates a websocket connection for the given change ID
// and websocket ID combination.
func (client *Client) getChangeWebsocket(changeID, websocketID string) (*websocket.Conn, error) {
	// Set up a new websocket dialer based on the HTTP client
	httpClient := client.doer.(*http.Client)
	httpTransport := httpClient.Transport.(*http.Transport)
	dialer := websocket.Dialer{
		NetDial:          httpTransport.Dial,
		Proxy:            httpTransport.Proxy,
		TLSClientConfig:  httpTransport.TLSClientConfig,
		HandshakeTimeout: 5 * time.Second,
	}

	// Establish the connection
	url := fmt.Sprintf("ws://localhost/v1/changes/%s/websocket?id=%s", changeID, url.QueryEscape(websocketID))
	conn, _, err := dialer.Dial(url, nil)
	if err != nil {
		return nil, err
	}
	return conn, err
}

type execCommand struct {
	Command string            `json:"command"`
	Args    map[string]string `json:"args"`
	Signal  int               `json:"signal"`
}

// ExecSendTermSize sends a window-resize message to the Exec control websocket.
func ExecSendTermSize(conn *websocket.Conn, width, height int) error {
	msg := execCommand{
		Command: "window-resize",
		Args: map[string]string{
			"width":  strconv.Itoa(width),
			"height": strconv.Itoa(height),
		},
	}
	return conn.WriteJSON(msg)
}

// ExecForwardSignal forwards a signal to the Exec control websocket.
func ExecForwardSignal(conn *websocket.Conn, signal int) error {
	msg := execCommand{
		Command: "signal",
		Signal:  signal,
	}
	return conn.WriteJSON(msg)
}
