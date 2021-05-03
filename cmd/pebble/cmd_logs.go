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
	"io/ioutil"
	"os"
	"os/signal"
	"strconv"
	"time"

	"github.com/jessevdk/go-flags"

	"github.com/canonical/pebble/client"
)

type cmdLogs struct {
	clientMixin
	Follow     bool   `short:"f" long:"follow"`
	Format     string `long:"format"`
	NumLogs    string `short:"n" long:"number"`
	Positional struct {
		Services []string `positional-arg-name:"<service>"`
	} `positional-args:"yes"`
}

var logsDescs = map[string]string{
	"follow": "Follow (tail) logs for given services until Ctrl-C pressed.",
	"format": `Output format: "text" (default) or "json" (JSON lines).`,
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
		if err != nil {
			return fmt.Errorf(`expected n to be a non-negative integer or "all", not %q`, cmd.NumLogs)
		}
	}

	var writeLog client.WriteLogFunc
	switch cmd.Format {
	case "", "text":
		writeLog = writeLogText
	case "json":
		writeLog = writeLogJSON
	default:
		return fmt.Errorf(`expected format to be "text" or "json", not %q`, cmd.Format)
	}

	opts := client.LogsOptions{
		WriteLog: writeLog,
		Services: cmd.Positional.Services,
		NumLogs:  &numLogs,
	}
	var err error
	if cmd.Follow {
		// Stop following when Ctrl-C pressed (SIGINT).
		ctx, _ := signal.NotifyContext(context.Background(), os.Interrupt)
		err = cmd.client.FollowLogs(ctx, &opts)
	} else {
		err = cmd.client.Logs(&opts)
	}
	return err
}

func writeLogText(timestamp time.Time, service string, stream client.LogStream, _ int, message io.Reader) error {
	b, err := ioutil.ReadAll(message)
	if err != nil {
		return err
	}
	if len(b) == 0 || b[len(b)-1] != '\n' {
		// Ensure we output a final newline
		b = append(b, '\n')
	}
	_, err = fmt.Printf("%s %s %s: %s", timestamp.Format(time.RFC3339), service, stream, b)
	return err
}

func writeLogJSON(timestamp time.Time, service string, stream client.LogStream, _ int, message io.Reader) error {
	b, err := ioutil.ReadAll(message)
	if err != nil {
		return err
	}
	var log = struct {
		Time    time.Time `json:"time"`
		Service string    `json:"service"`
		Stream  string    `json:"stream"`
		Message string    `json:"message"`
	}{
		Time:    timestamp,
		Service: service,
		Stream:  stream.String(),
		Message: string(b),
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(&log)
}

func init() {
	addCommand("logs", shortLogsHelp, longLogsHelp, func() flags.Commander { return &cmdLogs{} }, logsDescs, nil)
}
