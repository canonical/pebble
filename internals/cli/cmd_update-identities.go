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

const cmdUpdateIdentitiesSummary = "Update existing identities"
const cmdUpdateIdentitiesDescription = `
The update-identities command updates one or more existing identities.

The named identities must already exist.
`

type cmdUpdateIdentities struct {
	client *client.Client

	From string `long:"from" required:"1"`
}

func init() {
	AddCommand(&CmdInfo{
		Name:        "update-identities",
		Summary:     cmdUpdateIdentitiesSummary,
		Description: cmdUpdateIdentitiesDescription,
		ArgsHelp: map[string]string{
			"--from": "Path of YAML file to read identities from (required)",
		},
		New: func(opts *CmdOptions) flags.Commander {
			return &cmdUpdateIdentities{client: opts.Client}
		},
	})
}

func (cmd *cmdUpdateIdentities) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	identities, err := readIdentities(cmd.From)
	if err != nil {
		return err
	}
	err = cmd.client.UpdateIdentities(identities)
	if err != nil {
		return err
	}

	fmt.Fprintf(Stdout, "Updated %s\n", numItems(len(identities), "identity", "identities"))
	return nil
}
