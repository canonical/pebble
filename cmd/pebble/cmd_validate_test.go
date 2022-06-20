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

package main_test

import (
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	pebble "github.com/canonical/pebble/cmd/pebble"
)

func (s *PebbleSuite) TestValidateCommandYamlOK(c *C) {
	var err error

	layersDir := filepath.Join(s.pebbleDir, "layers")
	err = os.Mkdir(layersDir, 0755)
	c.Assert(err, IsNil)

	fpath := filepath.Join(layersDir, "001-kerncraft.yaml")
	err = ioutil.WriteFile(fpath, []byte("summary: Kerncraft Layer"), 0644)
	c.Assert(err, IsNil)

	_, err = pebble.Parser(pebble.Client()).ParseArgs([]string{"validate"})
	c.Assert(err, IsNil)
}

// TestValidateCommandYamlInvalidSchema does not attempt to test the plan validation
// logic. The purpose of this test is to ensure the plumbing is in place to report
// a YAML schema violation using the 'validate' command.
func (s *PebbleSuite) TestValidateCommandYamlInvalidSchema(c *C) {
	var err error

	layersDir := filepath.Join(s.pebbleDir, "layers")
	err = os.Mkdir(layersDir, 0755)
	c.Assert(err, IsNil)

	fpath := filepath.Join(layersDir, "001-kerncraft.yaml")
	err = ioutil.WriteFile(fpath, []byte("randomkey: Kerncraft Layer"), 0644)
	c.Assert(err, IsNil)

	_, err = pebble.Parser(pebble.Client()).ParseArgs([]string{"validate"})
	c.Assert(err, ErrorMatches, "cannot parse layer \"kerncraft\": yaml: unmarshal errors:\n"+
		"  line 1: field randomkey not found in type plan.Layer")
}
