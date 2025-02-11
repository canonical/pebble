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
	"strings"

	"github.com/canonical/go-flags"

	"github.com/canonical/pebble/client"
)

const cmdStartChecksSummary = "Start one or more checks"
const cmdStartChecksDescription = `
The start-checks command starts the configured health checks provided as
positional arguments. For any checks that are already active, the command
has no effect.
`

type cmdStartChecks struct {
	client *client.Client

	Positional struct {
		Checks []string `positional-arg-name:"<check>" required:"1"`
	} `positional-args:"yes"`
}

func init() {
	AddCommand(&CmdInfo{
		Name:        "start-checks",
		Summary:     cmdStartChecksSummary,
		Description: cmdStartChecksDescription,
		New: func(opts *CmdOptions) flags.Commander {
			return &cmdStartChecks{client: opts.Client}
		},
	})
}

func (cmd cmdStartChecks) Execute(args []string) error {
	if len(args) > 1 {
		return ErrExtraArgs
	}

	checkopts := client.ChecksActionOptions{
		Names: cmd.Positional.Checks,
	}
	results, err := cmd.client.StartChecks(&checkopts)
	if err != nil {
		return err
	}

	var summary string
	if len(results.Changed) == 0 {
		summary = fmt.Sprintf("Checks already started.")
	} else {
		summary = fmt.Sprintf("Checks started: %s", strings.Join(results.Changed, ", "))
	}

	fmt.Fprintln(Stdout, summary)
	return nil
}
