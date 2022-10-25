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
	"log"

	"github.com/canonical/pebble/client"
)

var runExample = false

func Example() {
	if !runExample {
		return // don't run this example under "go test"
	}

	pebble, err := client.New(&client.Config{Socket: ".pebble.socket"})
	if err != nil {
		log.Fatal(err)
	}
	changeID, err := pebble.Stop(&client.ServiceOptions{Names: []string{"mysvc"}})
	if err != nil {
		log.Fatal(err)
	}
	_, err = pebble.WaitChange(changeID, &client.WaitChangeOptions{})
	if err != nil {
		log.Fatal(err)
	}
}
