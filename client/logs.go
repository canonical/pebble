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
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"time"
)

type LogsOptions struct {
	// Function called to write a single log to the output (required). This
	// function must read the entire message till EOF.
	WriteLog WriteLogFunc

	// The list of service names to fetch logs for (nil or empty slice means
	// all services).
	Services []string

	// Total number of logs to fetch (before following if calling FollowLogs).
	// If nil, use Pebble's default (10). If negative, fetch all buffered logs.
	// If set to zero when calling FollowLogs, write no logs before following.
	NumLogs *int
}

type WriteLogFunc func(timestamp time.Time, service string, stream LogStream, length int, message io.Reader) error

type LogStream int

const (
	StreamUnknown LogStream = 0
	StreamStdout  LogStream = 1
	StreamStderr  LogStream = 2
)

func (s LogStream) String() string {
	switch s {
	case StreamStdout:
		return "stdout"
	case StreamStderr:
		return "stderr"
	default:
		return "unknown"
	}
}

// Logs fetches already-written logs from the given services.
func (client *Client) Logs(opts *LogsOptions) error {
	return client.logs(context.Background(), opts, false)
}

// FollowLogs requests logs from the given services and follows them until the
// context is cancelled.
func (client *Client) FollowLogs(ctx context.Context, opts *LogsOptions) error {
	return client.logs(ctx, opts, true)
}

func (client *Client) logs(ctx context.Context, opts *LogsOptions, follow bool) error {
	query := url.Values{}
	for _, service := range opts.Services {
		query.Add("services", service)
	}
	if opts.NumLogs != nil {
		query.Set("n", strconv.Itoa(*opts.NumLogs))
	}
	if follow {
		query.Set("follow", "true")
	}
	res, err := client.raw(ctx, "GET", "/v1/logs", query, nil, nil)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	reader := bufio.NewReader(res.Body)
	for {
		err = decodeLog(reader, opts.WriteLog)
		if errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) {
			break
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// Decode next log from reader and call writeLog on it. Return io.EOF if no more
// logs to read.
func decodeLog(reader *bufio.Reader, writeLog WriteLogFunc) error {
	// Read log metadata JSON and newline separator
	metaBytes, err := reader.ReadSlice('\n')
	if err == io.EOF {
		return io.EOF
	}
	if err != nil {
		return fmt.Errorf("cannot read log metadata: %w", err)
	}

	// Decode metadata
	var meta struct {
		Time    time.Time `json:"time"`
		Service string    `json:"service"`
		Stream  string    `json:"stream"`
		Length  int       `json:"length"`
	}
	err = json.Unmarshal(metaBytes, &meta)
	if err != nil {
		return fmt.Errorf("cannot unmarshal log metadata: %w", err)
	}
	stream := StreamUnknown
	switch meta.Stream {
	case "stdout":
		stream = StreamStdout
	case "stderr":
		stream = StreamStderr
	}

	// Read message bytes
	message := io.LimitReader(reader, int64(meta.Length))
	err = writeLog(meta.Time, meta.Service, stream, meta.Length, message)
	if err != nil {
		return fmt.Errorf("cannot output log: %w", err)
	}

	// Check that the LimitReader hit EOF, otherwise the call to writeLog
	// didn't read all the message bytes, and the bufio.Reader won't be in the
	// right place for the next read.
	var buf [1]byte
	_, err = message.Read(buf[:])
	if err == nil {
		return fmt.Errorf("WriteLog must read entire message")
	} else if err != io.EOF {
		return err
	}
	return nil
}
