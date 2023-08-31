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
	"context"
	"fmt"
	"os"
	"time"

	"github.com/canonical/go-flags"

	"github.com/canonical/pebble/client"
)

const cmdNoticesSummary = "List notices"
const cmdNoticesDescription = `
The notices command lists the notices that have occurred, ordered by the
last-repeated time.

If --timeout is given and matching notices aren't yet available, wait up to
the given duration for matching notices to arrive.
`

type cmdNotices struct {
	client *client.Client

	timeMixin
	Type    string        `long:"type"`
	Key     string        `long:"key"`
	After   string        `long:"after"`
	Timeout time.Duration `long:"timeout"`
}

func init() {
	AddCommand(&CmdInfo{
		Name:        "notices",
		Summary:     cmdNoticesSummary,
		Description: cmdNoticesDescription,
		ArgsHelp: merge(timeArgsHelp, map[string]string{
			"--type":    "Only list notices of this type",
			"--key":     "Only list notices with this key",
			"--after":   "Only list notices which occurred or were repeated after this time (RFC3339 format)",
			"--timeout": "Wait up to this duration for matching notices to arrive",
		}),
		New: func(opts *CmdOptions) flags.Commander {
			return &cmdNotices{client: opts.Client}
		},
	})
}

func (cmd *cmdNotices) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	var after time.Time
	var err error
	if cmd.After != "" {
		after, err = time.Parse(time.RFC3339, cmd.After)
		if err != nil {
			return fmt.Errorf("invalid --after timestamp: %w", err)
		}
	}
	options := client.NoticesOptions{
		Type:  cmd.Type,
		Key:   cmd.Key,
		After: after,
	}

	var notices []*client.Notice
	if cmd.Timeout != 0 {
		ctx := notifyContext(context.Background(), os.Interrupt)
		notices, err = cmd.client.WaitNotices(ctx, &options, cmd.Timeout)
	} else {
		notices, err = cmd.client.Notices(&options)
	}
	if err != nil {
		return err
	}

	if len(notices) == 0 {
		if cmd.Timeout != 0 {
			fmt.Fprintf(Stderr, "No matching notices after %s.\n", cmd.Timeout)
		} else {
			fmt.Fprintln(Stderr, "No matching notices.")
		}
		return nil
	}

	writer := tabWriter()
	defer writer.Flush()

	fmt.Fprintln(writer, "ID\tType\tKey\tFirst\tLast\tOccurrences\tRepeated")

	for _, notice := range notices {
		key := notice.Key
		if len(key) > 32 {
			// Truncate to 32 bytes with ellipsis in the middle
			key = key[:14] + "..." + key[len(key)-15:]
		}
		fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%d\t%s\n",
			notice.ID,
			notice.Type,
			key,
			cmd.fmtTime(notice.FirstOccurred),
			cmd.fmtTime(notice.LastOccurred),
			notice.Occurrences,
			cmd.fmtTime(notice.LastRepeated))
	}
	return nil
}
