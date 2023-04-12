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

package main

import (
	"github.com/canonical/go-flags"

	"github.com/canonical/pebble/client"
)

type cmdPlan struct {
	clientMixin
}

var shortPlanHelp = "Show the plan with layers combined"
var longPlanHelp = `
The plan command prints out the effective configuration of pebble in YAML
format. Layers are combined according to the override rules defined in them.
`

func (cmd *cmdPlan) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}
	planYAML, err := cmd.client.PlanBytes(&client.PlanOptions{})
	if err != nil {
		return err
	}
	Stdout.Write(planYAML)
	return nil
}

func init() {
	addCommand("plan", shortPlanHelp, longPlanHelp, func() flags.Commander { return &cmdPlan{} }, nil, nil)
}
