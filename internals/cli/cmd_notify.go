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

const cmdNotifySummary = "Record a custom notice"
const cmdNotifyDescription = `
The notify command records a custom notice with the specified key and optional
data fields.
`

type cmdNotify struct {
	client *client.Client

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
			"--repeat-after": "Prevent notice with same type and key from reoccurring within this duration",
		},
		New: func(opts *CmdOptions) flags.Commander {
			return &cmdNotify{client: opts.Client}
		},
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
		Type:        client.CustomNotice,
		Key:         cmd.Positional.Key,
		RepeatAfter: cmd.RepeatAfter,
		Data:        data,
	}
	noticeId, err := cmd.client.Notify(&options)
	if err != nil {
		return err
	}
	fmt.Fprintf(Stdout, "Recorded notice %s\n", noticeId)
	return nil
}
