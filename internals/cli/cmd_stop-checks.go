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

	"github.com/canonical/pebble/client"
)

const cmdStopChecksSummary = "Stop one or more checks"
const cmdStopChecksDescription = `
The stop-checks command stops the configured health checks provided as
positional arguments. For any checks that are inactive, the command has
no effect.
`

type cmdStopChecks struct {
	client *client.Client

	Positional struct {
		Checks []string `positional-arg-name:"<check>" required:"1"`
	} `positional-args:"yes"`
}

func init() {
	AddCommand(&CmdInfo{
		Name:        "stop-checks",
		Summary:     cmdStopChecksSummary,
		Description: cmdStopChecksDescription,
		New: func(opts *CmdOptions) flags.Commander {
			return &cmdStopChecks{client: opts.Client}
		},
	})
}

func (cmd cmdStopChecks) Execute(args []string) error {
	if len(args) > 1 {
		return ErrExtraArgs
	}

	checkopts := client.ChecksOptions{
		Names: cmd.Positional.Checks,
	}
	response, err := cmd.client.StopChecks(&checkopts)
	if err != nil {
		return err
	}

	fmt.Fprintln(Stdout, response)
	return nil
}
