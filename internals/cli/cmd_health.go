// Copyright (c) 2023 Canonical Ltd
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

const cmdHealthSummary = "Query health of checks"
const cmdHealthDescription = `
The health command queries the health of configured checks.

It returns an exit code 0 if all the requested checks are healthy, or
an exit code 1 if at least one of the requested checks are unhealthy.
`

type cmdHealth struct {
	client *client.Client

	//lint:ignore SA5008 "choice" tag is intentionally duplicated
	Level      string `long:"level" choice:"alive" choice:"ready"`
	Positional struct {
		Checks []string `positional-arg-name:"<check>"`
	} `positional-args:"yes"`
}

var cmdHealthArgsHelp = map[string]string{
	"--level": "Check level to filter for",
}

func init() {
	AddCommand(&CmdInfo{
		Name:        "health",
		Summary:     cmdHealthSummary,
		Description: cmdHealthDescription,
		ArgsHelp:    cmdHealthArgsHelp,
		New: func(opts *CmdOptions) flags.Commander {
			return &cmdHealth{client: opts.Client}
		},
	})
}

func (cmd *cmdHealth) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	opts := client.HealthOptions{
		Level: client.CheckLevel(cmd.Level),
		Names: cmd.Positional.Checks,
	}
	health, err := cmd.client.Health(&opts)
	if err != nil {
		return err
	}

	status := "unhealthy"
	if health {
		status = "healthy"
	}
	fmt.Fprintln(Stdout, status)

	if !health {
		panic(&exitStatus{1})
	}

	return nil
}
