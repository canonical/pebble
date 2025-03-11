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
	"strings"

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

	fmt.Fprintln(w, "Check\tLevel\tStartup\tStatus\tFailures\tChange")

	for _, check := range checks {
		level := check.Level
		if level == client.UnsetLevel {
			level = "-"
		}
		failures := "-"
		if check.Status != client.CheckStatusInactive {
			failures = fmt.Sprintf("%d/%d", check.Failures, check.Threshold)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			check.Name, level, check.Startup, check.Status, failures,
			cmd.changeInfo(check))
	}
	return nil
}

func (cmd *cmdChecks) changeInfo(check *client.CheckInfo) string {
	if check.ChangeID == "" {
		return "-"
	}
	// Only include last task log if check is failing.
	if check.Failures == 0 {
		return check.ChangeID
	}
	log, err := cmd.lastTaskLog(check.ChangeID)
	if err != nil {
		return fmt.Sprintf("%s (%v)", check.ChangeID, err)
	}
	if log == "" {
		return check.ChangeID
	}
	// Truncate to limited number of bytes with ellipsis and "for more" text.
	const maxError = 70
	if len(log) > maxError {
		forMore := fmt.Sprintf(`... run "pebble tasks %s" for more`, check.ChangeID)
		log = log[:maxError-len(forMore)] + forMore
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
	// Strip initial "<timestamp> ERROR|INFO" text from log.
	lastLog := logs[len(logs)-1]
	fields := strings.SplitN(lastLog, " ", 3)
	if len(fields) > 2 {
		lastLog = fields[2]
	}
	lastLog = strings.ReplaceAll(lastLog, "\n", "\\n")
	return lastLog, nil
}
