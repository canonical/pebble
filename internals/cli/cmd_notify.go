// Copyright (c) 2023 Canonical Ltd
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

package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/canonical/go-flags"

	"github.com/canonical/pebble/client"
)

const cmdNotifySummary = "Record a client notice"
const cmdNotifyDescription = `
The notify command records a "client" notice with the given key and optional
data fields.
`

type cmdNotify struct {
	clientMixin
	RepeatAfter time.Duration `long:"repeat-after"`
	Positional  struct {
		Key  string   `positional-arg-name:"<key>" required:"1"`
		Data []string `positional-arg-name:"<name=value>"`
	} `positional-args:"yes"`
}

func init() {
	AddCommand(&CmdInfo{
		Name:        "notify",
		Summary:     cmdNotifySummary,
		Description: cmdNotifyDescription,
		ArgsHelp: map[string]string{
			"--repeat-after": "If set, allow the notice to repeat after this duration",
		},
		Builder: func() flags.Commander { return &cmdNotify{} },
	})
}

func (cmd *cmdNotify) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	data := make(map[string]string, len(cmd.Positional.Data))
	for _, kv := range cmd.Positional.Data {
		key, value, ok := strings.Cut(kv, "=")
		if !ok {
			return fmt.Errorf(`data args must be in "name=value" format, not %q`, kv)
		}
		data[key] = value
	}
	options := client.NotifyOptions{
		Key:         cmd.Positional.Key,
		RepeatAfter: cmd.RepeatAfter,
		Data:        data,
	}
	return cmd.client.Notify(&options)
}
