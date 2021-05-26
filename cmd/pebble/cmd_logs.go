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

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"time"

	"github.com/jessevdk/go-flags"

	"github.com/canonical/pebble/client"
)

const (
	logTimeFormat = "2006-01-02T15:04:05.000Z07:00"
)

type cmdLogs struct {
	clientMixin
	Follow     bool   `short:"f" long:"follow"`
	Output     string `short:"o" long:"output"`
	NumLogs    string `short:"n" long:"number"`
	Positional struct {
		Services []string `positional-arg-name:"<service>"`
	} `positional-args:"yes"`
}

var logsDescs = map[string]string{
	"follow": "Follow (tail) logs for given services until Ctrl-C pressed.",
	"output": "Output format: \"text\" (default), \"json\" (JSON lines), or \n\"raw\" (copy raw log bytes to stdout and stderr).",
	"number": "Number of logs to show (before following); defaults to 10.\nIf 'all', show all buffered logs.",
}

var shortLogsHelp = "Fetch service logs"
var longLogsHelp = `
The logs command fetches buffered logs from the given services (or all services
if none are specified) and displays them in chronological order.
`

func (cmd *cmdLogs) Execute(args []string) error {
	var numLogs int
	switch cmd.NumLogs {
	case "":
		numLogs = 10
	case "all":
		numLogs = -1
	default:
		var err error
		numLogs, err = strconv.Atoi(cmd.NumLogs)
		if err != nil || numLogs < 0 {
			return fmt.Errorf(`expected n to be a non-negative integer or "all", not %q`, cmd.NumLogs)
		}
	}

	var writeLog client.WriteLogFunc
	switch cmd.Output {
	case "", "text":
		writeLog = writeLogText
	case "json":
		writeLog = writeLogJSON
	case "raw":
		writeLog = writeLogRaw
	default:
		return fmt.Errorf(`invalid output format (expected "json", "text", or "raw", not %q)`, cmd.Output)
	}

	opts := client.LogsOptions{
		WriteLog: writeLog,
		Services: cmd.Positional.Services,
		NumLogs:  &numLogs,
	}
	var err error
	if cmd.Follow {
		// Stop following when Ctrl-C pressed (SIGINT).
		ctx := notifyContext(context.Background(), os.Interrupt)
		err = cmd.client.FollowLogs(ctx, &opts)
	} else {
		err = cmd.client.Logs(&opts)
	}
	return err
}

// Needed because signal.NotifyContext is Go 1.16+
func notifyContext(parent context.Context, signals ...os.Signal) context.Context {
	ctx, cancel := context.WithCancel(parent)
	ch := make(chan os.Signal)
	signal.Notify(ch, signals...)
	go func() {
		// Wait for signal, then cancel the context.
		<-ch
		cancel()
	}()
	return ctx
}

func writeLogText(timestamp time.Time, service, message string) error {
	suffix := ""
	if len(message) == 0 || message[len(message)-1] != '\n' {
		suffix = "\n"
	}
	_, err := fmt.Fprintf(Stdout, "%s [%s] %s%s",
		timestamp.Format(logTimeFormat), service, message, suffix)
	return err
}

func writeLogRaw(timestamp time.Time, service, message string) error {
	_, err := io.WriteString(Stdout, message)
	return err
}

func writeLogJSON(timestamp time.Time, service, message string) error {
	var log = struct {
		Time    time.Time `json:"time"`
		Service string    `json:"service"`
		Message string    `json:"message"`
	}{
		Time:    timestamp,
		Service: service,
		Message: message,
	}
	encoder := json.NewEncoder(Stdout)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(&log)
}

func init() {
	addCommand("logs", shortLogsHelp, longLogsHelp, func() flags.Commander { return &cmdLogs{} }, logsDescs, nil)
}
