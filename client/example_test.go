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

package client_test

import (
	"fmt"

	"github.com/canonical/pebble/client"
)

var runExample = false

func Example() {
	if !runExample {
		return // don't run this example under "go test"
	}

	pebble, err := client.New(&client.Config{Socket: ".pebble.socket"})
	if err != nil {
		fmt.Println(err)
		return
	}
	files, err := pebble.ListFiles(&client.ListFilesOptions{Path: "/tmp"})
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, file := range files {
		fmt.Println(file.Name())
	}
}
