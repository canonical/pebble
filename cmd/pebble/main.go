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

	"github.com/canonical/pebble/client"
	"github.com/canonical/pebble/internals/cli"
)

func main() {
	cliOptions := &cli.RunOptions{
		ClientConfig: &client.Config{
			Socket:  os.Getenv("PEBBLE_SOCKET"),
			BaseURL: os.Getenv("PEBBLE_BASEURL"),
		},
		PebbleDir: os.Getenv("PEBBLE"),
	}

	if err := cli.Run(cliOptions); err != nil {
		fmt.Fprintf(cli.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
