// Copyright (c) 2022 Canonical Ltd
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

	"github.com/canonical/pebble/internal/plan"
)

var shortValidateHelp = "Validate daemon configuration"
var longValidateHelp = `
Validate the Pebble configuration layers in the $PEBBLE/layers directory,
exiting with an error message and non-zero exit code on failure.
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
	return err
}
