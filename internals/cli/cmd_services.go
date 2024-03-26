// Copyright (c) 2021 Canonical Ltd
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

const cmdServicesSummary = "Query the status of configured services"
const cmdServicesDescription = `
The services command lists status information about the services specified, or
about all services if none are specified.
`

type cmdServices struct {
	client *client.Client

	timeMixin
	Positional struct {
		Services []string `positional-arg-name:"<service>"`
	} `positional-args:"yes"`
}

func init() {
	AddCommand(&CmdInfo{
		Name:        "services",
		Summary:     cmdServicesSummary,
		Description: cmdServicesDescription,
		ArgsHelp:    timeArgsHelp,
		New: func(opts *CmdOptions) flags.Commander {
			return &cmdServices{client: opts.Client}
		},
	})
}

func (cmd *cmdServices) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	opts := client.ServicesOptions{
		Names: cmd.Positional.Services,
	}
	services, err := cmd.client.Services(&opts)
	if err != nil {
		return err
	}
	if len(services) == 0 {
		if len(cmd.Positional.Services) == 0 {
			fmt.Fprintln(Stderr, "Plan has no services.")
		} else {
			fmt.Fprintln(Stderr, "No matching services.")
		}
		return nil
	}

	w := tabWriter()
	defer w.Flush()

	fmt.Fprintln(w, "Service\tStartup\tCurrent\tSince")

	for _, svc := range services {
		since := "-"
		if !svc.CurrentSince.IsZero() {
			since = cmd.fmtTime(svc.CurrentSince)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", svc.Name, svc.Startup, svc.Current, since)
	}
	return nil
}
