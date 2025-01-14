// Copyright (c) 2024 Canonical Ltd
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

const cmdRunCheckSummary = "Run check immediately and get the status"
const cmdRunCheckDescription = `
The check command runs a check immediately and return the status.
`

type cmdRunCheck struct {
	client *client.Client

	Positional struct {
		Check string `positional-arg-name:"<check>" required:"1"`
	} `positional-args:"yes"`
}

func init() {
	AddCommand(&CmdInfo{
		Name:        "run-check",
		Summary:     cmdRunCheckSummary,
		Description: cmdRunCheckDescription,
		New: func(opts *CmdOptions) flags.Commander {
			return &cmdRunCheck{client: opts.Client}
		},
	})
}

func (cmd *cmdRunCheck) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	opts := client.CheckPayload{
		Action: "run",
		Check:  cmd.Positional.Check,
	}
	status, err := cmd.client.RunCheck(&opts)
	if err != nil {
		return err
	}
	fmt.Fprintln(Stdout, status)
	return nil
}
