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
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/canonical/go-flags"
	"gopkg.in/yaml.v3"

	"github.com/canonical/pebble/client"
)

const cmdAddIdentitiesSummary = "Add new identities"
const cmdAddIdentitiesDescription = `
The add-identities command adds one or more new identities.

The named identities must not yet exist.

For example, to add a local admin named "bob", use YAML like this:

> identities:
>     bob:
>         access: admin
>         local:
>             user-id: 42

To add an identity named "alice" with metrics access using HTTP basic authentication:

> identities:
>     alice:
>         access: metrics
>         basic:
>             password: <password hash>

Use "openssl passwd -6" to generate a hashed password (sha512-crypt format).
`

type cmdAddIdentities struct {
	client *client.Client

	From string `long:"from" required:"1"`
}

func init() {
	AddCommand(&CmdInfo{
		Name:        "add-identities",
		Summary:     cmdAddIdentitiesSummary,
		Description: cmdAddIdentitiesDescription,
		ArgsHelp: map[string]string{
			"--from": "Path of YAML file to read identities from (required)",
		},
		New: func(opts *CmdOptions) flags.Commander {
			return &cmdAddIdentities{client: opts.Client}
		},
	})
}

func (cmd *cmdAddIdentities) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	identities, err := readIdentities(cmd.From)
	if err != nil {
		return err
	}
	err = cmd.client.AddIdentities(identities)
	if err != nil {
		return err
	}

	fmt.Fprintf(Stdout, "Added %s.\n", numItems(len(identities), "new identity", "new identities"))
	return nil
}

func readIdentities(path string) (map[string]*client.Identity, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var identities identitiesMap
	err = yaml.Unmarshal(data, &identities)
	if err != nil {
		return nil, fmt.Errorf("cannot unmarshal identities: %w", err)
	}
	if len(identities.Identities) == 0 {
		return nil, errors.New(`no identities to add; did you forget the top-level "identities" key?`)
	}
	return identities.Identities, nil
}

func numItems(n int, singular, plural string) string {
	if n == 1 {
		return "1 " + singular
	}
	return strconv.Itoa(n) + " " + plural
}
