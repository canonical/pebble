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

package cli_test

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/cli"
)

func (s *PebbleSuite) TestAdd(c *check.C) {
	for _, combine := range []bool{false, true} {
		triggerLayerContent := "trigger layer"
		layerYAML := `
services:
   foo:
    override: replace
    command: cmd
`[1:]

		s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
			c.Check(r.Method, check.Equals, "POST")
			c.Check(r.URL.Path, check.Equals, "/v1/layers")

			body := DecodedRequestBody(c, r)

			layerContent, ok := body["layer"]
			c.Assert(ok, check.Equals, true)

			if layerContent == triggerLayerContent {
				fmt.Fprint(w, `{
    "type": "error",
    "result": {
		"message": "triggered"
	}
}`)
			} else {
				c.Check(body, check.DeepEquals, map[string]any{
					"action":  "add",
					"combine": combine,
					"label":   "foo",
					"format":  "yaml",
					"layer":   layerYAML,
					"inner":   false,
				})
				fmt.Fprint(w, `{
    "type": "sync",
    "status-code": 200,
    "result": true
}`)
			}

		})

		tempDir := c.MkDir()
		layerPath := filepath.Join(tempDir, "layer.yaml")
		err := os.WriteFile(layerPath, []byte(layerYAML), 0755)
		c.Assert(err, check.IsNil)

		unreadableLayerPath := filepath.Join(tempDir, "unreadable-layer.yaml")
		err = os.WriteFile(unreadableLayerPath, []byte(layerYAML), 0055)
		c.Assert(err, check.IsNil)

		// The trigger layer will trigger an error in the mocked API response
		triggerLayerPath := filepath.Join(tempDir, "trigger-layer.yaml")
		err = os.WriteFile(triggerLayerPath, []byte(triggerLayerContent), 0755)
		c.Assert(err, check.IsNil)

		var args []string
		for _, path := range []string{layerPath, unreadableLayerPath, triggerLayerPath} {
			args = []string{"add"}
			if combine {
				args = append(args, "--combine")
			}
			args = append(args, "foo", path)
			rest, err := cli.ParserForTest().ParseArgs(args)

			if path == layerPath {
				c.Assert(err, check.IsNil)
				c.Assert(rest, check.HasLen, 0)
				c.Check(s.Stdout(), check.Matches, `Layer "foo" added successfully.*\n`)
				c.Check(s.Stderr(), check.Equals, "")
				s.ResetStdStreams()
			} else if path == triggerLayerPath {
				c.Assert(err, check.ErrorMatches, "triggered")
			} else if path == unreadableLayerPath {
				if os.Getuid() != 0 {
					c.Assert(os.IsPermission(err), check.Equals, true)
				}
			}
		}

		args = append(args, "extra", "arguments", "invalid")
		_, err = cli.ParserForTest().ParseArgs(args)
		c.Assert(err, check.Equals, cli.ErrExtraArgs)
		s.ResetStdStreams()
	}
}
