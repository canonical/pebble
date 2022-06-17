// Copyright (c) 2014-2020 Canonical Ltd
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
	"os"

	"github.com/canonical/pebble/internal/plan"
	"github.com/jessevdk/go-flags"
)

var shortValidateHelp = "Validate daemon configration"
var longValidateHelp = `
Perform validation of daemon configuration files and exit.
`

type cmdValidate struct {
	clientMixin
}

func init() {
	addCommand("validate", shortValidateHelp, longValidateHelp, func() flags.Commander { return &cmdValidate{} }, nil, nil)
}

func (cmd cmdValidate) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	pebbleDir, _ := getEnvPaths()

	_, err := plan.ReadDir(pebbleDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "configuration validation failed: %v\n", err)
		panic(&exitStatus{1})
	}

	return nil
}
