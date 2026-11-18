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
	"github.com/canonical/go-flags"

	"github.com/canonical/pebble/client"
)

var cmdPlanSummary = "Show the plan with layers combined"
var cmdPlanDescription = `
The plan command prints out the effective configuration of {{.DisplayName}} in YAML
or CNF format. Layers are combined according to the override rules defined in them.
`

type cmdPlan struct {
	client *client.Client

	//lint:ignore SA5008 "choice" tag is intentionally duplicated
	Format string `long:"format" default:"yaml" choice:"yaml" choice:"cnf"`
}

func init() {
	AddCommand(&CmdInfo{
		Name:        "plan",
		Summary:     cmdPlanSummary,
		Description: cmdPlanDescription,
		ArgsHelp: map[string]string{
			"--format": "Output format",
		},
		New: func(opts *CmdOptions) flags.Commander {
			return &cmdPlan{client: opts.Client}
		},
	})
}

func (cmd *cmdPlan) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}
	planData, err := cmd.client.PlanBytes(&client.PlanOptions{Format: cmd.Format})
	if err != nil {
		return err
	}
	Stdout.Write(planData)
	return nil
}
