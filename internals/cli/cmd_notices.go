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
	"os/signal"
	"strconv"
	"time"

	"github.com/canonical/go-flags"

	"github.com/canonical/pebble/client"
)

const cmdNoticesSummary = "List notices"
const cmdNoticesDescription = `
The notices command lists notices not yet acknowledged, ordered by the
last-repeated time (oldest first). After it runs, the notices that were shown
may then be acknowledged by running '{{.ProgramName}} okay'. When a notice repeats, it
needs to be acknowledged again.

By default, list notices with the current user ID or public notices. Admins
can use --users=all to view notice with any user ID, or --uid=UID to view
another user's notices.
`

type cmdNotices struct {
	client *client.Client

	socketPath string

	timeMixin
	Users   client.NoticesUsers `long:"users"`
	UID     *uint32             `long:"uid"`
	Type    []client.NoticeType `long:"type"`
	Key     []string            `long:"key"`
	Timeout time.Duration       `long:"timeout"`
}

func init() {
	AddCommand(&CmdInfo{
		Name:        "notices",
		Summary:     cmdNoticesSummary,
		Description: cmdNoticesDescription,
		ArgsHelp: merge(timeArgsHelp, map[string]string{
			"--users":   "The only valid value is 'all', which lists notices with any user ID (admin only; cannot be used with --uid)",
			"--uid":     "Only list notices with this user ID (admin only; cannot be used with --users)",
			"--type":    "Only list notices of this type (multiple allowed)",
			"--key":     "Only list notices with this key (multiple allowed)",
			"--timeout": "Wait up to this duration for matching notices to arrive",
		}),
		New: func(opts *CmdOptions) flags.Commander {
			return &cmdNotices{
				client:     opts.Client,
				socketPath: opts.SocketPath,
			}
		},
	})
}

func (cmd *cmdNotices) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	state, err := loadCLIState(cmd.socketPath)
	if err != nil {
		return fmt.Errorf("cannot load CLI state: %w", err)
	}
	options := client.NoticesOptions{
		Users:  cmd.Users,
		UserID: cmd.UID,
		Types:  cmd.Type,
		Keys:   cmd.Key,
		After:  state.NoticesLastOkayed,
	}

	var notices []*client.Notice
	if cmd.Timeout != 0 {
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
		defer cancel()
		notices, err = cmd.client.WaitNotices(ctx, cmd.Timeout, &options)
	} else {
		notices, err = cmd.client.Notices(&options)
	}
	if err != nil {
		return err
	}

	if len(notices) == 0 {
		if cmd.Timeout != 0 {
			fmt.Fprintf(Stderr, "No matching notices after waiting %s.\n", cmd.Timeout)
		} else {
			fmt.Fprintln(Stderr, "No matching notices.")
		}
		return nil
	}

	writer := tabWriter()
	defer writer.Flush()

	fmt.Fprintln(writer, "ID\tUser\tType\tKey\tFirst\tRepeated\tOccurrences")

	for _, notice := range notices {
		key := notice.Key
		if len(key) > 32 {
			// Truncate to 32 bytes with ellipsis in the middle
			key = key[:14] + "..." + key[len(key)-15:]
		}
		userIDStr := "public"
		if notice.UserID != nil {
			userIDStr = strconv.FormatUint(uint64(*notice.UserID), 10)
		}
		fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%s\t%d\n",
			notice.ID,
			userIDStr,
			notice.Type,
			key,
			cmd.fmtTime(notice.FirstOccurred),
			cmd.fmtTime(notice.LastRepeated),
			notice.Occurrences)
	}

	state.NoticesLastListed = notices[len(notices)-1].LastRepeated
	err = saveCLIState(cmd.socketPath, state)
	if err != nil {
		return fmt.Errorf("cannot save CLI state: %w", err)
	}
	return nil
}
