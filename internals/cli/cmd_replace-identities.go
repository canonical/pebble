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

const cmdReplaceIdentitiesSummary = "Replace identities"
const cmdReplaceIdentitiesDescription = `
The replace-identities command replaces, adds, or removes one or more
identities.

If a named identity exists, it will be updated. If it does not exist, it will
be added. If a named identity is null in the YAML input, that identity will be
removed.
`

type cmdReplaceIdentities struct {
	client *client.Client

	From string `long:"from" required:"1"`
}

func init() {
	AddCommand(&CmdInfo{
		Name:        "replace-identities",
		Summary:     cmdReplaceIdentitiesSummary,
		Description: cmdReplaceIdentitiesDescription,
		ArgsHelp: map[string]string{
			"--from": "Path of YAML file to read identities from (required)",
		},
		New: func(opts *CmdOptions) flags.Commander {
			return &cmdReplaceIdentities{client: opts.Client}
		},
	})
}

func (cmd *cmdReplaceIdentities) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	identities, err := readIdentities(cmd.From)
	if err != nil {
		return err
	}
	err = cmd.client.ReplaceIdentities(identities)
	if err != nil {
		return err
	}

	fmt.Fprintf(Stdout, "Replaced %s\n", numItems(len(identities), "identity", "identities"))
	return nil
}
