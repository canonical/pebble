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
	"sort"
	"strings"

	"github.com/canonical/go-flags"

	"github.com/canonical/pebble/client"
)

const cmdIdentitiesSummary = "List identities"
const cmdIdentitiesDescription = `
The identities command lists all identities.

Other identity-related subcommands are as follows (use --help with any
subcommand for details):

{{.ProgramName}} identity           Show a single identity
{{.ProgramName}} add-identities     Add new identities
{{.ProgramName}} update-identities  Update or replace identities
{{.ProgramName}} remove-identities  Remove identities
`

type cmdIdentities struct {
	client *client.Client

	formatMixin
}

func init() {
	AddCommand(&CmdInfo{
		Name:        "identities",
		Summary:     cmdIdentitiesSummary,
		Description: cmdIdentitiesDescription,
		ArgsHelp:    formatArgsHelp,
		New: func(opts *CmdOptions) flags.Commander {
			return &cmdIdentities{client: opts.Client}
		},
	})
}

type identitiesMap struct {
	Identities map[string]*client.Identity `json:"identities" yaml:"identities"`
}

func (cmd *cmdIdentities) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	identities, err := cmd.client.Identities(nil)
	if err != nil {
		return err
	}

	if cmd.Format == "text" {
		if len(identities) == 0 {
			fmt.Fprintln(Stderr, "No identities.")
			return nil
		}
		return cmd.writeText(identities)
	}

	if identities == nil {
		identities = map[string]*client.Identity{}
	}
	return cmd.formatNonText(identitiesMap{Identities: identities})
}

func (cmd *cmdIdentities) writeText(identities map[string]*client.Identity) error {
	writer := tabWriter()
	defer writer.Flush()

	fmt.Fprintln(writer, "Name\tAccess\tTypes")

	// Sort by name to ensure stable output.
	var names []string
	for name := range identities {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		identity := identities[name]

		var types []string
		if identity.Local != nil {
			types = append(types, "local")
		}
		if identity.Basic != nil {
			types = append(types, "basic")
		}
		sort.Strings(types)
		if len(types) == 0 {
			types = append(types, "unknown")
		}

		fmt.Fprintf(writer, "%s\t%s\t%s\n", name, identity.Access, strings.Join(types, ","))
	}
	return nil
}
