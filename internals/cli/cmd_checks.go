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
	clientMixin
	Level      string `long:"level"`
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
			"--level": `Check level to filter for ("alive" or "ready")`,
		},
		Builder: func() flags.Commander { return &cmdChecks{} },
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

	fmt.Fprintln(w, "Check\tLevel\tStatus\tFailures")

	for _, check := range checks {
		level := check.Level
		if level == client.UnsetLevel {
			level = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%d/%d\n", check.Name, level, check.Status, check.Failures, check.Threshold)
	}
	return nil
}
