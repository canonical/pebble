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

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strconv"

	"github.com/canonical/go-flags"

	"github.com/canonical/pebble/client"
)

const cmdLogsSummary = "Fetch service logs"
const cmdLogsDescription = `
The logs command fetches buffered logs from the given services (or all services
if none are specified) and displays them in chronological order.
`

type cmdLogs struct {
	client *client.Client

	Follow     bool   `short:"f" long:"follow"`
	Format     string `long:"format"`
	N          string `short:"n"`
	Positional struct {
		Services []string `positional-arg-name:"<service>"`
	} `positional-args:"yes"`
}

func init() {
	AddCommand(&CmdInfo{
		Name:        "logs",
		Summary:     cmdLogsSummary,
		Description: cmdLogsDescription,
		ArgsHelp: map[string]string{
			"--follow": "Follow (tail) logs for given services until Ctrl-C is\npressed. If no services are specified, show logs from\nall services running when the command starts.",
			"--format": "Output format: \"text\" (default) or \"json\" (JSON lines).",
			"-n":       "Number of logs to show (before following); defaults to 30.\nIf 'all', show all buffered logs.",
		},
		New: func(opts *CmdOptions) flags.Commander {
			return &cmdLogs{client: opts.Client}
		},
	})
}

const (
	logTimeFormat = "2006-01-02T15:04:05.000Z07:00"
)

func (cmd *cmdLogs) Execute(args []string) error {
	var n int
	switch cmd.N {
	case "":
		n = 30
	case "all":
		n = -1
	default:
		var err error
		n, err = strconv.Atoi(cmd.N)
		if err != nil || n < 0 {
			return fmt.Errorf(`expected n to be a non-negative integer or "all", not %q`, cmd.N)
		}
	}

	var writeLog func(entry client.LogEntry) error
	switch cmd.Format {
	case "", "text":
		writeLog = func(entry client.LogEntry) error {
			_, err := fmt.Fprintf(Stdout, "%s [%s] %s\n",
				entry.Time.Format(logTimeFormat), entry.Service, entry.Message)
			return err
		}

	case "json":
		encoder := json.NewEncoder(Stdout)
		encoder.SetEscapeHTML(false)
		writeLog = func(entry client.LogEntry) error {
			return encoder.Encode(&entry)
		}

	default:
		return fmt.Errorf(`invalid output format (expected "json" or "text", not %q)`, cmd.Format)
	}

	opts := client.LogsOptions{
		WriteLog: writeLog,
		Services: cmd.Positional.Services,
		N:        n,
	}
	var err error
	if cmd.Follow {
		// Stop following when Ctrl-C pressed (SIGINT).
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
		defer cancel()
		err = cmd.client.FollowLogs(ctx, &opts)
	} else {
		err = cmd.client.Logs(&opts)
	}
	return err
}
