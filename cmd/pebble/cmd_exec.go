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
	"os"
	"time"

	"github.com/jessevdk/go-flags"

	"github.com/canonical/pebble/client"
)

type cmdExec struct {
	clientMixin
}

var shortExecHelp = "TODO"
var longExecHelp = `TODO`

func (cmd *cmdExec) Execute(args []string) error {
	fmt.Printf("cmdExec args = %q\n", args)
	opts := &client.ExecOptions{
		Command: args,
		//TODO
		//Environment: env,
		//User:        c.flagUser,
		//Group:       c.flagGroup,
		//WorkingDir: "",
	}

	additionalArgs := &client.ExecAdditionalArgs{
		Stdin:    os.Stdin,
		Stdout:   os.Stdout,
		Stderr:   os.Stderr,
		Control:  nil, // TODO
		DataDone: make(chan bool),
	}

	changeID, err := cmd.client.Exec(opts, additionalArgs)
	if err != nil {
		return err
	}

	// TODO: add /v1/changes/{id}/wait and use that instead
	var returnCode int
	for {
		ch, err := cmd.client.Change(changeID)
		if err != nil {
			return err
		}
		fmt.Printf("TODO: change = %+v\n", ch)
		if ch.Ready {
			if ch.Err != "" {
				fmt.Printf("TODO: ready with error: %v\n", ch.Err)
				break
			}
			err := ch.Get("return", &returnCode)
			if err != nil {
				return err
			}
			fmt.Printf("TODO: ready, returnCode = %d\n", returnCode)
			break
		}
		time.Sleep(250 * time.Millisecond)
	}

	// Wait for any remaining I/O to be flushed
	<-additionalArgs.DataDone

	return nil
}

func init() {
	addCommand("exec", shortExecHelp, longExecHelp, func() flags.Commander { return &cmdExec{} }, nil, nil)
}
