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

	"github.com/gorilla/websocket"

	"github.com/canonical/pebble/internal/logger"
	"github.com/canonical/pebble/internal/wsutil"
)

// TODO: don't tie JSON / API type to this
type ExecOptions struct {
	Command     []string          `json:"command"`
	Environment map[string]string `json:"environment"`
	WorkingDir  string            `json:"working-dir"`
	UserID      *int              `json:"user-id"`
	User        string            `json:"user"`
	GroupID     *int              `json:"group-id"`
	Group       string            `json:"group"`
}

type ExecAdditionalArgs struct {
	// Standard input
	Stdin io.ReadCloser

	// Standard output
	Stdout io.WriteCloser

	// Standard error
	Stderr io.WriteCloser

	// Control message handler (window resize, signals, ...)
	Control func(conn *websocket.Conn)

	// Channel that will be closed when all data operations are done
	DataDone chan bool
}

func (client *Client) getChangeWebsocket(changeID, secret string) (*websocket.Conn, error) {
	url := fmt.Sprintf("ws://localhost/v1/exec/%s/websocket?secret=%s", changeID, url.QueryEscape(secret))

	httpClient := client.doer.(*http.Client)
	httpTransport := httpClient.Transport.(*http.Transport)

	// Setup a new websocket dialer based on it
	dialer := websocket.Dialer{
		//lint:ignore SA1019 DialContext doesn't exist in Go 1.13
		NetDial:         httpTransport.Dial,
		Proxy:           httpTransport.Proxy,
		TLSClientConfig: httpTransport.TLSClientConfig,
		//TODO: should we have a timeout?
		//HandshakeTimeout:  7*time.Second,
	}

	// Set the user agent
	headers := http.Header{}

	// Establish the connection
	fmt.Printf("TODO dialing %s\n", url)
	conn, _, err := dialer.Dial(url, headers)
	if err != nil {
		fmt.Printf("TODO dial error: %v\n", err)
		return nil, err
	}

	// Log the data
	logger.Debugf("Connected to the websocket: %v", url)

	return conn, err
}

func (client *Client) Exec(opts *ExecOptions, args *ExecAdditionalArgs) (changeID string, err error) {
	data, err := json.Marshal(opts)
	if err != nil {
		return "", err
	}
	headers := map[string]string{
		"Content-Type": "application/json",
	}
	resultBytes, changeID, err := client.doAsyncFull("POST", "/v1/exec", nil, headers, bytes.NewReader(data))
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
	fmt.Printf("TODO: result = %+v\n", result)

	// Process additional arguments
	if args != nil {
		fds := result.WebsocketIDs

		// Call the control handler with a connection to the control socket
		if args.Control != nil && fds["control"] != "" {
			conn, err := client.getChangeWebsocket(changeID, fds["control"])
			if err != nil {
				return "", err
			}

			go args.Control(conn)
		}

		if false /* TODO: opts.Interactive*/ {
			// Handle interactive sections
			if args.Stdin != nil && args.Stdout != nil {
				// Connect to the websocket
				conn, err := client.getChangeWebsocket(changeID, fds["0"])
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
			dones := map[int]chan bool{}
			conns := []*websocket.Conn{}

			// Handle stdin
			fmt.Printf("TODO: websockets\n")
			if fds["0"] != "" {
				conn, err := client.getChangeWebsocket(changeID, fds["0"])
				if err != nil {
					fmt.Printf("TODO: websocket 0 error: %v\n", err)
					return "", err
				}
				fmt.Printf("TODO: websocket 0 success\n")

				conns = append(conns, conn)
				dones[0] = wsutil.WebsocketSendStream(conn, args.Stdin, -1)
				fmt.Printf("TODO: websocket 0 send\n")
			}

			// Handle stdout
			if fds["1"] != "" {
				conn, err := client.getChangeWebsocket(changeID, fds["1"])
				if err != nil {
					fmt.Printf("TODO: websocket 1 error: %v\n", err)
					return "", err
				}
				fmt.Printf("TODO: websocket 1 success\n")

				conns = append(conns, conn)
				dones[1] = wsutil.WebsocketRecvStream(args.Stdout, conn)
				fmt.Printf("TODO: websocket 1 recv\n")
			}

			// Handle stderr
			if fds["2"] != "" {
				conn, err := client.getChangeWebsocket(changeID, fds["2"])
				if err != nil {
					fmt.Printf("TODO: websocket 2 error: %v\n", err)
					return "", err
				}
				fmt.Printf("TODO: websocket 2 success\n")

				conns = append(conns, conn)
				dones[2] = wsutil.WebsocketRecvStream(args.Stderr, conn)
				fmt.Printf("TODO: websocket 2 recv\n")
			}

			// Wait for everything to be done
			go func() {
				for i, chDone := range dones {
					// Skip stdin, dealing with it separately below
					if i == 0 {
						continue
					}

					<-chDone
				}

				if fds["0"] != "" {
					if args.Stdin != nil {
						args.Stdin.Close()
					}

					// Empty the stdin channel but don't block on it as
					// stdin may be stuck in Read()
					go func() {
						<-dones[0]
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
	}

	return changeID, nil
}
