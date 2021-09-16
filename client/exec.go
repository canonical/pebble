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
	"io/ioutil"
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

	// Standard input stream. If nil, no input is sent.
	Stdin io.Reader

	// Standard output stream. If nil, output is discarded.
	Stdout io.Writer

	// Standard error stream. If nil, error output is discarded.
	Stderr io.Writer
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

// Execution represents a running command. Use Wait to wait for it to finish.
type Execution struct {
	changeID         string
	client           *Client
	timeout          time.Duration
	dataDone         chan struct{}
	controlWebsocket jsonWriter
}

// jsonWriter makes it easier to write tests for SendSignal and SendResize.
type jsonWriter interface {
	WriteJSON(v interface{}) error
}

// Exec starts a command execution with the given options, returning a value
// representing the execution.
func (client *Client) Exec(opts *ExecOptions) (*Execution, error) {
	stdin := opts.Stdin
	if stdin == nil {
		stdin = bytes.NewReader(nil)
	}
	stdout := opts.Stdout
	if stdout == nil {
		stdout = ioutil.Discard
	}
	var stderr io.Writer
	if opts.SeparateStderr {
		stderr = opts.Stderr
		if stderr == nil {
			stderr = ioutil.Discard
		}
	} else if stderr != nil {
		return nil, fmt.Errorf("Stderr must be nil if SeparateStderr is false")
	}

	// Hit the /v1/exec endpoint to start the command.
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
		return nil, err
	}
	headers := map[string]string{
		"Content-Type": "application/json",
	}
	resultBytes, changeID, err := client.doAsyncFull("POST", "/v1/exec", nil, headers, &body)
	if err != nil {
		return nil, err
	}
	var result execResult
	err = json.Unmarshal(resultBytes, &result)
	if err != nil {
		return nil, err
	}

	wsIDs := result.WebsocketIDs
	if wsIDs["control"] == "" {
		return nil, fmt.Errorf(`response did not include "control" websocket`)
	}
	if wsIDs["io"] == "" {
		return nil, fmt.Errorf(`response did not include "io" websocket`)
	}
	if opts.SeparateStderr && wsIDs["stderr"] == "" {
		return nil, fmt.Errorf(`response did not include "stderr" websocket`)
	}

	controlWebsocket, err := client.getChangeWebsocket(changeID, wsIDs["control"])
	if err != nil {
		return nil, err
	}
	closeControl := func() {
		closeMsg := websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")
		controlWebsocket.WriteMessage(websocket.CloseMessage, closeMsg)
		controlWebsocket.Close()
	}

	dataDone := make(chan struct{})

	if opts.UseTerminal {
		// Handle terminal-based executions
		conn, err := client.getChangeWebsocket(changeID, wsIDs["io"])
		if err != nil {
			return nil, err
		}

		// And attach stdin and stdout to it
		go func() {
			wsutil.WebsocketSendStream(conn, stdin, -1)
			<-wsutil.WebsocketRecvStream(stdout, conn)
			conn.Close()
			close(dataDone)
			closeControl()
		}()
	} else {
		// Handle non-terminal executions
		dones := map[string]chan bool{}
		conns := []*websocket.Conn{}

		// Handle stdin and stdout
		conn, err := client.getChangeWebsocket(changeID, wsIDs["io"])
		if err != nil {
			return nil, err
		}
		conns = append(conns, conn)
		dones["stdin"] = wsutil.WebsocketSendStream(conn, stdin, -1)
		dones["stdout"] = wsutil.WebsocketRecvStream(stdout, conn)

		// Handle stderr separately if needed
		if opts.SeparateStderr {
			conn, err := client.getChangeWebsocket(changeID, wsIDs["stderr"])
			if err != nil {
				return nil, err
			}
			conns = append(conns, conn)
			dones["stderr"] = wsutil.WebsocketRecvStream(stderr, conn)
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

			if wsIDs["io"] != "" {
				// Empty the stdin channel but don't block on it as
				// stdin may be stuck in Read()
				go func() {
					<-dones["stdin"]
				}()
			}

			for _, conn := range conns {
				conn.Close()
			}

			close(dataDone)
			closeControl()
		}()
	}

	execution := &Execution{
		changeID:         changeID,
		client:           client,
		timeout:          opts.Timeout,
		dataDone:         dataDone,
		controlWebsocket: controlWebsocket,
	}
	return execution, nil
}

// Wait waits for the command execution to finish and returns its exit code.
func (e *Execution) Wait() (int, error) {
	// Wait till the command (change) is finished.
	waitOpts := &WaitChangeOptions{}
	if e.timeout != 0 {
		// A little more than the command timeout to ensure that happens first
		waitOpts.Timeout = e.timeout + time.Second
	}
	change, err := e.client.WaitChange(e.changeID, waitOpts)
	if err != nil {
		return 0, err
	}
	if change.Err != "" {
		return 0, errors.New(change.Err)
	}
	var exitCode int
	err = change.Get("exit-code", &exitCode)
	if err != nil {
		return 0, err
	}

	// Wait for any remaining I/O to be flushed.
	<-e.dataDone

	return exitCode, nil
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

// SendResize sends a resize message to this command execution.
func (e *Execution) SendResize(width, height int) error {
	msg := execCommand{
		Command: "resize",
		Resize: &execResizeArgs{
			Width:  width,
			Height: height,
		},
	}
	return e.controlWebsocket.WriteJSON(msg)
}

// SendSignal sends a signal to this command execution.
func (e *Execution) SendSignal(signal string) error {
	msg := execCommand{
		Command: "signal",
		Signal: &execSignalArgs{
			Name: signal,
		},
	}
	return e.controlWebsocket.WriteJSON(msg)
}
