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

package main_test

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"path/filepath"

	"gopkg.in/check.v1"

	pebble "github.com/canonical/pebble/cmd/pebble"
)

func assertBodyEquals(c *check.C, body io.ReadCloser, expected map[string]interface{}) {
	defer body.Close()
	var actual map[string]interface{}
	err := json.NewDecoder(body).Decode(&actual)
	c.Assert(err, check.IsNil)
	c.Assert(actual, check.DeepEquals, expected)
}

func (s *PebbleSuite) TestAdd(c *check.C) {
	layerYAML := `
services:
 foo:
  override: replace
  command: cmd
`[1:]

	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, check.Equals, "POST")
		c.Check(r.URL.Path, check.Equals, "/v1/layers")
		assertBodyEquals(c, r.Body, map[string]interface{}{
			"action": "combine",
			"format": "yaml",
			"layer":  layerYAML,
		})
		fmt.Fprint(w, `{
    "type": "sync",
    "status-code": 200,
    "result": true
}`)
	})

	tempDir := c.MkDir()
	layerPath := filepath.Join(tempDir, "layer.yaml")
	err := ioutil.WriteFile(layerPath, []byte(layerYAML), 0755)
	c.Assert(err, check.IsNil)

	rest, err := pebble.Parser(pebble.Client()).ParseArgs([]string{"add", layerPath})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.HasLen, 0)
	c.Check(s.Stdout(), check.Matches, `Layer added successfully.*\n`)
	c.Check(s.Stderr(), check.Equals, "")
}
