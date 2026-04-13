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
	"gopkg.in/yaml.v3"

	"github.com/canonical/pebble/client"
)

const cmdIdentitySummary = "Show a single identity"
const cmdIdentityDescription = `
The identity command shows details for a single identity in YAML format.
`

type cmdIdentity struct {
	client *client.Client

	Positional struct {
		Name string `positional-arg-name:"<name>" required:"1"`
	} `positional-args:"yes"`
}

func init() {
	AddCommand(&CmdInfo{
		Name:        "identity",
		Summary:     cmdIdentitySummary,
		Description: cmdIdentityDescription,
		New: func(opts *CmdOptions) flags.Commander {
			return &cmdIdentity{client: opts.Client}
		},
	})
}

func (cmd *cmdIdentity) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	identities, err := cmd.client.Identities(nil)
	if err != nil {
		return err
	}
	identity, ok := identities[cmd.Positional.Name]
	if !ok {
		return fmt.Errorf("cannot find identity %q", cmd.Positional.Name)
	}
	data, err := yaml.Marshal(identity)
	if err != nil {
		return err
	}
	fmt.Fprint(Stdout, string(data))
	return nil
}
