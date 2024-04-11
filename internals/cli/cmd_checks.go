// Copyright (c) 2022 Canonical Ltd
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
	"fmt"

	"github.com/canonical/go-flags"

	"github.com/canonical/pebble/client"
)

const cmdChecksSummary = "Query the status of configured health checks"
const cmdChecksDescription = `
The checks command lists status information about the configured health
checks, optionally filtered by level and check names provided as positional
arguments.
`

type cmdChecks struct {
	client *client.Client

	Level      string `long:"level" choice:"alive" choice:"ready"`
	Positional struct {
		Checks []string `positional-arg-name:"<check>"`
	} `positional-args:"yes"`
}

func init() {
	AddCommand(&CmdInfo{
		Name:        "checks",
		Summary:     cmdChecksSummary,
		Description: cmdChecksDescription,
		ArgsHelp: map[string]string{
			"--level": "Check level to filter for",
		},
		New: func(opts *CmdOptions) flags.Commander {
			return &cmdChecks{client: opts.Client}
		},
	})
}

func (cmd *cmdChecks) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	opts := client.ChecksOptions{
		Level: client.CheckLevel(cmd.Level),
		Names: cmd.Positional.Checks,
	}
	checks, err := cmd.client.Checks(&opts)
	if err != nil {
		return err
	}
	if len(checks) == 0 {
		if len(cmd.Positional.Checks) == 0 && cmd.Level == "" {
			fmt.Fprintln(Stderr, "Plan has no health checks.")
		} else {
			fmt.Fprintln(Stderr, "No matching health checks.")
		}
		return nil
	}

	w := tabWriter()
	defer w.Flush()

	fmt.Fprintln(w, "Check\tLevel\tStatus\tFailures\tChange")

	for _, check := range checks {
		level := check.Level
		if level == client.UnsetLevel {
			level = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%d/%d\t%s\n",
			check.Name, level, check.Status, check.Failures,
			check.Threshold, cmd.changeInfo(check))
	}
	return nil
}

func (cmd *cmdChecks) changeInfo(check *client.CheckInfo) string {
	if check.ChangeID == "" {
		return "-"
	}
	// Only include last task log if check is down.
	if check.Status != client.CheckStatusDown {
		return check.ChangeID
	}
	log, err := cmd.lastTaskLog(check.ChangeID)
	if err != nil {
		return fmt.Sprintf("%s (ERROR: %v)", check.ChangeID, err)
	}
	if log == "" {
		return check.ChangeID
	}
	// Truncate to 50 bytes with ellipsis in the middle.
	if len(log) > 50 {
		log = log[:23] + "..." + log[len(log)-24:]
	}
	return fmt.Sprintf("%s (%s)", check.ChangeID, log)
}

func (cmd *cmdChecks) lastTaskLog(changeID string) (string, error) {
	change, err := cmd.client.Change(changeID)
	if err != nil {
		return "", err
	}
	if len(change.Tasks) < 1 {
		return "", nil
	}
	logs := change.Tasks[0].Log
	if len(logs) < 1 {
		return "", nil
	}
	return logs[len(logs)-1], nil
}
