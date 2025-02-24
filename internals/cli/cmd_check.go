// Copyright (c) 2025 Canonical Ltd
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
	"gopkg.in/yaml.v3"

	"github.com/canonical/pebble/client"
)

const cmdCheckSummary = "Query the details of a configured health check"
const cmdCheckDescription = `
The check command shows details for a single check in YAML format.
`

type cmdCheck struct {
	client *client.Client

	Positional struct {
		Check string `positional-arg-name:"<check>" required:"1"`
	} `positional-args:"yes"`
}

func init() {
	AddCommand(&CmdInfo{
		Name:        "check",
		Summary:     cmdCheckSummary,
		Description: cmdCheckDescription,
		New: func(opts *CmdOptions) flags.Commander {
			return &cmdCheck{client: opts.Client}
		},
	})
}

func (cmd *cmdCheck) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	opts := client.ChecksOptions{
		Names: []string{cmd.Positional.Check},
	}
	checks, err := cmd.client.Checks(&opts)
	if err != nil {
		return err
	}
	if len(checks) == 0 {
		fmt.Fprintln(Stderr, "No matching health checks.")
		return nil
	}

	check := checks[0]
	data, err := yaml.Marshal(check)
	if err != nil {
		return err
	}
	fmt.Fprint(Stdout, string(data))
	return nil
}

func (cmd *cmdCheck) taskLogs(changeID string) ([]string, error) {
	var logs []string
	change, err := cmd.client.Change(changeID)
	if err != nil {
		return logs, err
	}
	if len(change.Tasks) < 1 {
		return logs, nil
	}
	logs = change.Tasks[0].Log
	return logs, nil
}
