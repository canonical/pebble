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
	"fmt"
	"io/ioutil"

	"github.com/jessevdk/go-flags"

	"github.com/canonical/pebble/client"
)

type cmdMerge struct {
	clientMixin
	Positional struct {
		LayerPath string `positional-arg-name:"<layer-path>" required:"1"`
	} `positional-args:"yes"`
}

var shortMergeHelp = "Dynamically merge a layer on top of the plan's layers"
var longMergeHelp = `
The merge command reads the plan's layer YAML from the path specified, and
merges it on top of the current dynamic layer (which are on top of any static
layers loaded when Pebble started). If there are no dynamic layers, a new
dynamic layer is added.
`

func (cmd *cmdMerge) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}
	layerYAML, err := ioutil.ReadFile(cmd.Positional.LayerPath)
	if err != nil {
		return err
	}
	err = cmd.client.MergeLayer(&client.MergeLayerOptions{Layer: string(layerYAML)})
	if err != nil {
		return err
	}
	fmt.Fprintf(Stdout, "Dynamic layer added successfully from %q\n", cmd.Positional.LayerPath)
	return nil
}

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
	layerYAML, err := cmd.client.GetPlan(&client.GetPlanOptions{})
	if err != nil {
		return err
	}
	fmt.Fprint(Stdout, layerYAML)
	return nil
}

func init() {
	addCommand("merge", shortMergeHelp, longMergeHelp, func() flags.Commander { return &cmdMerge{} }, nil, nil)
	addCommand("plan", shortPlanHelp, longPlanHelp, func() flags.Commander { return &cmdPlan{} }, nil, nil)
}
