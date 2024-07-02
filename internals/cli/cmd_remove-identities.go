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

const cmdRemoveIdentitiesSummary = "Remove identities"
const cmdRemoveIdentitiesDescription = `
The remove-identities command removes one or more identities.

The named identities must exist. The named identity entries must be null in
the YAML input. For example, to remove "alice" and "bob", use this YAML:

> identities:
>     alice: null
>     bob: null
`

type cmdRemoveIdentities struct {
	client *client.Client

	From string `long:"from" required:"1"`
}

func init() {
	AddCommand(&CmdInfo{
		Name:        "remove-identities",
		Summary:     cmdRemoveIdentitiesSummary,
		Description: cmdRemoveIdentitiesDescription,
		ArgsHelp: map[string]string{
			"--from": "Path of YAML file to read identities from (required)",
		},
		New: func(opts *CmdOptions) flags.Commander {
			return &cmdRemoveIdentities{client: opts.Client}
		},
	})
}

func (cmd *cmdRemoveIdentities) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	identities, err := readIdentities(cmd.From)
	if err != nil {
		return err
	}
	identityNames := make(map[string]struct{}, len(identities))
	for name, identity := range identities {
		if identity != nil {
			return fmt.Errorf("identity value for %q must be null for remove operation", name)
		}
		identityNames[name] = struct{}{}
	}
	err = cmd.client.RemoveIdentities(identityNames)
	if err != nil {
		return err
	}

	fmt.Fprintf(Stdout, "Removed %s.\n", numItems(len(identities), "identity", "identities"))
	return nil
}
