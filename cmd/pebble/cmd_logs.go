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
	"github.com/jessevdk/go-flags"
)

type cmdLogs struct {
	clientMixin
	Positional struct {
		Services []string `positional-arg-name:"<service>"`
	} `positional-args:"yes"`
}

var logsDescs = map[string]string{}

var shortLogsHelp = "Retrieve logs for service"
var longLogsHelp = `
The logs command fetches logs of the given services and displays them in
chronological order.
`

func (cmd *cmdLogs) Execute(args []string) error {

	return nil
}

func init() {
	addCommand("logs", shortLogsHelp, longLogsHelp, func() flags.Commander { return &cmdLogs{} }, logsDescs, nil)
}
