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

type cmdAdd struct {
	clientMixin
	Action     string `long:"action"`
	Positional struct {
		LayerPath string `positional-arg-name:"<layer-path>" required:"1"`
	} `positional-args:"yes"`
}

var addDescs = map[string]string{
	"action": `Method used to add the layer: only "combine" (the default) is supported right now`,
}

var shortAddHelp = "Dynamically add a layer to the plan's layers"
var longAddHelp = `
The add command reads the plan's layer YAML from the path specified, and
combines it with the current dynamic layer (which in turn are on top of any
static layers loaded when Pebble started). If there are no dynamic layers,
a new dynamic layer is added.
`

func (cmd *cmdAdd) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}
	data, err := ioutil.ReadFile(cmd.Positional.LayerPath)
	if err != nil {
		return err
	}
	opts := client.AddLayerOptions{
		Action:    client.AddLayerAction(cmd.Action),
		LayerData: data,
	}
	if opts.Action == "" {
		opts.Action = client.AddLayerCombine
	}
	err = cmd.client.AddLayer(&opts)
	if err != nil {
		return err
	}
	fmt.Fprintf(Stdout, "Layer added successfully from %q\n", cmd.Positional.LayerPath)
	return nil
}

func init() {
	addCommand("add", shortAddHelp, longAddHelp, func() flags.Commander { return &cmdAdd{} }, addDescs, nil)
}
