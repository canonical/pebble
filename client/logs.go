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

const (
	logReaderSize = 4 * 1024
)

// LogsOptions holds the options for a call to Logs or FollowLogs.
type LogsOptions struct {
	// WriteLog is called to write a single log to the output (required).
	WriteLog func(entry LogEntry) error

	// Services is the list of service names to fetch logs for (nil or empty
	// slice means all services).
	Services []string

	// N defines the number of log lines to return from the buffer. In follow
	// mode, the default is zero, in non-follow mode it's server-defined
	// (currently 30). Set to -1 to return the entire buffer.
	N int
}

// LogEntry is the struct passed to the WriteLog function.
type LogEntry struct {
	Time    time.Time `json:"time"`
	Service string    `json:"service"`
	Message string    `json:"message"`
}

// Logs fetches previously-written logs from the given services.
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
	if opts.N != 0 {
		query.Set("n", strconv.Itoa(opts.N))
	}
	if follow {
		query.Set("follow", "true")
	}
	res, err := client.raw(ctx, "GET", "/v1/logs", query, nil, nil)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	reader := bufio.NewReaderSize(res.Body, logReaderSize)
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

// Decode next JSON log from reader and call writeLog on it. Return io.EOF if
// no more logs to read.
func decodeLog(reader *bufio.Reader, writeLog func(entry LogEntry) error) error {
	// Read log JSON and newline separator
	b, err := reader.ReadSlice('\n')
	if errors.Is(err, io.EOF) {
		return io.EOF
	}
	if err != nil {
		return fmt.Errorf("cannot read log line: %w", err)
	}

	var entry LogEntry
	err = json.Unmarshal(b, &entry)
	if err != nil {
		return fmt.Errorf("cannot unmarshal log: %w", err)
	}

	err = writeLog(entry)
	if err != nil {
		return fmt.Errorf("cannot output log: %w", err)
	}
	return nil
}
