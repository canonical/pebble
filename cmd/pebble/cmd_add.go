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
	Combine    bool `long:"combine"`
	Positional struct {
		Label     string `positional-arg-name:"<label>" required:"1"`
		LayerPath string `positional-arg-name:"<layer-path>" required:"1"`
	} `positional-args:"yes"`
}

var addDescs = map[string]string{
	"combine": `Combine the new layer with an existing layer that has the given label (default is to append)`,
}

var shortAddHelp = "Dynamically add a layer to the plan's layers"
var longAddHelp = `
The add command reads the plan's layer YAML from the path specified and
appends a layer with the given label to the plan's layers. If --combine
is specified, combine the layer with an existing layer that has the given
label (or append if the label is not found).
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
		Combine:   cmd.Combine,
		Label:     cmd.Positional.Label,
		LayerData: data,
	}
	err = cmd.client.AddLayer(&opts)
	if err != nil {
		return err
	}
	fmt.Fprintf(Stdout, "Layer %q added successfully from %q\n",
		cmd.Positional.Label, cmd.Positional.LayerPath)
	return nil
}

func init() {
	addCommand("add", shortAddHelp, longAddHelp, func() flags.Commander { return &cmdAdd{} }, addDescs, nil)
}
