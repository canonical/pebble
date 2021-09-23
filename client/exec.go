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

// ExecOptions are the options for an Exec call.
type ExecOptions struct {
	// Required: command and arguments (first element is the executable).
	Command []string

	// Optional environment variables.
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

	// Standard error stream. If nil, error output is combined with standard
	// output and goes to the Stdout stream.
	Stderr io.Writer
}

type execPayload struct {
	Command       []string          `json:"command"`
	Environment   map[string]string `json:"environment"`
	WorkingDir    string            `json:"working-dir"`
	Timeout       string            `json:"timeout"`
	UserID        *int              `json:"user-id"`
	User          string            `json:"user"`
	GroupID       *int              `json:"group-id"`
	Group         string            `json:"group"`
	UseTerminal   bool              `json:"use-terminal"`
	CombineStderr bool              `json:"combine-stderr"`
	Width         int               `json:"width"`
	Height        int               `json:"height"`
}

type execResult struct {
	WebsocketIDs map[string]string `json:"websocket-ids"`
}

// ExecProcess represents a running process. Use Wait to wait for it to finish.
type ExecProcess struct {
	changeID    string
	client      *Client
	timeout     time.Duration
	writesDone  chan struct{}
	controlConn jsonWriter
}

// jsonWriter makes it easier to write tests for SendSignal and SendResize.
type jsonWriter interface {
	WriteJSON(v interface{}) error
}

// Exec starts a command with the given options, returning a value
// representing the process.
func (client *Client) Exec(opts *ExecOptions) (*ExecProcess, error) {
	// Set up stdin/stdout defaults.
	stdin := opts.Stdin
	if stdin == nil {
		stdin = bytes.NewReader(nil)
	}
	stdout := opts.Stdout
	if stdout == nil {
		stdout = ioutil.Discard
	}

	// Call the /v1/exec endpoint to start the command.
	var timeoutStr string
	if opts.Timeout != 0 {
		timeoutStr = opts.Timeout.String()
	}
	payload := execPayload{
		Command:       opts.Command,
		Environment:   opts.Environment,
		WorkingDir:    opts.WorkingDir,
		Timeout:       timeoutStr,
		UserID:        opts.UserID,
		User:          opts.User,
		GroupID:       opts.GroupID,
		Group:         opts.Group,
		UseTerminal:   opts.UseTerminal,
		CombineStderr: opts.Stderr == nil,
		Width:         opts.Width,
		Height:        opts.Height,
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

	// Check that we have the websocket IDs we expect.
	wsIDs := result.WebsocketIDs
	if wsIDs["control"] == "" {
		return nil, fmt.Errorf(`exec response missing "control" websocket`)
	}
	if wsIDs["io"] == "" {
		return nil, fmt.Errorf(`exec response missing "io" websocket`)
	}
	if opts.Stderr != nil && wsIDs["stderr"] == "" {
		return nil, fmt.Errorf(`exec response missing "stderr" websocket`)
	}

	// Connect to the "control" websocket.
	controlConn, err := client.getChangeWebsocket(changeID, wsIDs["control"])
	if err != nil {
		return nil, err
	}

	// Forward stdin and stdout.
	ioConn, err := client.getChangeWebsocket(changeID, wsIDs["io"])
	if err != nil {
		return nil, err
	}
	stdinDone := wsutil.WebsocketSendStream(ioConn, stdin, -1)
	stdoutDone := wsutil.WebsocketRecvStream(stdout, ioConn)

	// Handle stderr separately if needed.
	var stderrConn *websocket.Conn
	var stderrDone chan bool
	if opts.Stderr != nil {
		stderrConn, err = client.getChangeWebsocket(changeID, wsIDs["stderr"])
		if err != nil {
			return nil, err
		}
		stderrDone = wsutil.WebsocketRecvStream(opts.Stderr, stderrConn)
	}

	// Fire up a goroutine to wait for everything to be done.
	writesDone := make(chan struct{})
	go func() {
		// Wait till the WebsocketRecvStream goroutines are done writing to
		// stdout and stderr. This happens when EOF is signalled or websocket
		// is closed.
		<-stdoutDone
		if stderrDone != nil {
			<-stderrDone
		}

		// Empty the stdin channel, but don't block on it as stdin may be
		// stuck in Read. This is due to the somewhat poor design of
		// WebsocketSendStream, which writes to an unbuffered channel
		// (instead of closing it or using a buffered channel). We'd rather
		// not modify that package much, so it's closer to the LXD code.
		go func() {
			<-stdinDone // happens when reading stdin returns EOF
		}()

		// Try to close websocket connections gracefully, but ignore errors.
		_ = ioConn.Close()
		if stderrConn != nil {
			_ = stderrConn.Close()
		}
		_ = controlConn.Close()

		// Tell ExecProcess.Wait we're done writing to stdout/stderr.
		close(writesDone)
	}()

	process := &ExecProcess{
		changeID:    changeID,
		client:      client,
		timeout:     opts.Timeout,
		writesDone:  writesDone,
		controlConn: controlConn,
	}
	return process, nil
}

// Wait waits for the command process to finish and returns its exit code.
func (p *ExecProcess) Wait() (int, error) {
	// Wait till the command (change) is finished.
	waitOpts := &WaitChangeOptions{}
	if p.timeout != 0 {
		// A little more than the command timeout to ensure that happens first
		waitOpts.Timeout = p.timeout + time.Second
	}
	change, err := p.client.WaitChange(p.changeID, waitOpts)
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

	// Wait for any remaining I/O to be flushed to stdout/stderr.
	<-p.writesDone

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

// SendResize sends a resize message to the running process.
func (p *ExecProcess) SendResize(width, height int) error {
	msg := execCommand{
		Command: "resize",
		Resize: &execResizeArgs{
			Width:  width,
			Height: height,
		},
	}
	return p.controlConn.WriteJSON(msg)
}

// SendSignal sends a signal to the running process.
func (p *ExecProcess) SendSignal(signal string) error {
	msg := execCommand{
		Command: "signal",
		Signal: &execSignalArgs{
			Name: signal,
		},
	}
	return p.controlConn.WriteJSON(msg)
}
