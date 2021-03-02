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
	"github.com/jessevdk/go-flags"

	"github.com/canonical/pebble/client"
)

type cmdPlan struct {
	clientMixin
}

var shortPlanHelp = "Show the plan with layers combined"
var longPlanHelp = `
The plan command reads the plan (configuration) and displays it as YAML. The
plan's layers are flattened according to Pebble's layer override rules.
`

func (cmd *cmdPlan) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}
	data, err := cmd.client.PlanData(&client.PlanDataOptions{})
	if err != nil {
		return err
	}
	Stdout.Write(data)
	return nil
}

func init() {
	addCommand("plan", shortPlanHelp, longPlanHelp, func() flags.Commander { return &cmdPlan{} }, nil, nil)
}
