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
	"fmt"
	"io"
	"net/http"
	"net/url"
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
	UseTerminal bool

	// True to separate the process's stderr into a separate websocket. The
	// default is to send combined stdout+stderr on a single websocket.
	//
	// Note: currently only the combinations UseTerminal=true,
	// SeparateStderr=false and UseTerminal=false, SeparateStderr=true are
	// supported.
	SeparateStderr bool

	// Initial terminal width and height (only apply if UseTerminal is true).
	// If not specified, the Pebble server uses the target's default (usually
	// 80x25). When using the "pebble exec" CLI, these are set to the host's
	// terminal size automatically.
	Width  int
	Height int

	// Standard input, output, and error streams.
	Stdin  io.ReadCloser
	Stdout io.Writer
	Stderr io.Writer

	// Control message handler (for window resizing and signal forwarding)
	Control func(conn WebsocketWriter)

	// Channel that will be closed when all data operations are done
	DataDone chan bool
}

type execPayload struct {
	Command        []string          `json:"command"`
	Environment    map[string]string `json:"environment"`
	WorkingDir     string            `json:"working-dir"`
	Timeout        string            `json:"timeout"`
	UserID         *int              `json:"user-id"`
	User           string            `json:"user"`
	GroupID        *int              `json:"group-id"`
	Group          string            `json:"group"`
	UseTerminal    bool              `json:"use-terminal"`
	SeparateStderr bool              `json:"separate-stderr"`
	Width          int               `json:"width"`
	Height         int               `json:"height"`
}

type execResult struct {
	WebsocketIDs map[string]string `json:"websocket-ids"`
}

// Exec starts a command execution with the given options and additional
// control arguments, returning the execution's change ID.
func (client *Client) Exec(opts *ExecOptions) (string, error) {
	var timeoutStr string
	if opts.Timeout != 0 {
		timeoutStr = opts.Timeout.String()
	}
	payload := execPayload{
		Command:        opts.Command,
		Environment:    opts.Environment,
		WorkingDir:     opts.WorkingDir,
		Timeout:        timeoutStr,
		UserID:         opts.UserID,
		User:           opts.User,
		GroupID:        opts.GroupID,
		Group:          opts.Group,
		UseTerminal:    opts.UseTerminal,
		SeparateStderr: opts.SeparateStderr,
		Width:          opts.Width,
		Height:         opts.Height,
	}
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
	var result execResult
	err = json.Unmarshal(resultBytes, &result)
	if err != nil {
		return "", err
	}

	// Process additional arguments (connecting I/O and websockets)
	fds := result.WebsocketIDs

	// Call the control handler with a connection to the control socket
	if opts.Control != nil && fds["control"] != "" {
		conn, err := client.getChangeWebsocket(changeID, fds["control"])
		if err != nil {
			return "", err
		}
		go opts.Control(conn)
	}

	if opts.UseTerminal {
		// Handle terminal-based executions
		if opts.Stdin != nil && opts.Stdout != nil {
			// Connect to the websocket
			conn, err := client.getChangeWebsocket(changeID, fds["io"])
			if err != nil {
				return "", err
			}

			// And attach stdin and stdout to it
			go func() {
				wsutil.WebsocketSendStream(conn, opts.Stdin, -1)
				<-wsutil.WebsocketRecvStream(opts.Stdout, conn)
				conn.Close()
				if opts.DataDone != nil {
					close(opts.DataDone)
				}
			}()
		} else {
			if opts.DataDone != nil {
				close(opts.DataDone)
			}
		}
	} else {
		// Handle non-terminal executions
		dones := map[string]chan bool{}
		conns := []*websocket.Conn{}

		// Handle stdin and stdout
		if fds["io"] != "" {
			conn, err := client.getChangeWebsocket(changeID, fds["io"])
			if err != nil {
				return "", err
			}
			conns = append(conns, conn)
			dones["stdin"] = wsutil.WebsocketSendStream(conn, opts.Stdin, -1)
			dones["stdout"] = wsutil.WebsocketRecvStream(opts.Stdout, conn)
		}

		// Handle stderr separately if needed
		if opts.SeparateStderr && fds["stderr"] != "" {
			conn, err := client.getChangeWebsocket(changeID, fds["stderr"])
			if err != nil {
				return "", err
			}
			conns = append(conns, conn)
			dones["stderr"] = wsutil.WebsocketRecvStream(opts.Stderr, conn)
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
				if opts.Stdin != nil {
					opts.Stdin.Close()
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

			if opts.DataDone != nil {
				close(opts.DataDone)
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

// WebsocketWriter is a websocket writer interface that can write a websocket
// or a value as JSON, for example, for sending commands to an executing
// program's "control" websocket.
type WebsocketWriter interface {
	WriteMessage(messageType int, data []byte) error
	WriteJSON(v interface{}) error
}

type execCommand struct {
	Command string          `json:"command"`
	Signal  *execSignalArgs `json:"signal,omitempty"`
	Resize  *execResizeArgs `json:"resize,omitempty"`
}

type execSignalArgs struct {
	Name string `json:"name"`
}

type execResizeArgs struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// ExecSendResize sends a resize message to the Exec control websocket.
func ExecSendResize(conn WebsocketWriter, width, height int) error {
	msg := execCommand{
		Command: "resize",
		Resize: &execResizeArgs{
			Width:  width,
			Height: height,
		},
	}
	return conn.WriteJSON(msg)
}

// ExecSendSignal sends a signal to the Exec control websocket.
func ExecSendSignal(conn WebsocketWriter, signal string) error {
	msg := execCommand{
		Command: "signal",
		Signal: &execSignalArgs{
			Name: signal,
		},
	}
	return conn.WriteJSON(msg)
}
