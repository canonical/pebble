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
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/canonical/go-flags"

	"github.com/canonical/pebble/client"
	"github.com/canonical/pebble/internals/osutil"
)

const (
	noticesFilenameEnvKey  = "PEBBLE_NOTICES_FILENAME"
	noticesFilenameDefault = "~/.pebble/notices_<socket-path>.json"
)

const cmdNoticesSummary = "List notices"
const cmdNoticesDescription = `
The notices command lists the notices that have occurred, ordered by the
Repeated time (oldest first).

If --timeout is given and matching notices aren't yet available, wait up to
the given duration for matching notices to arrive, then return them.

By default, this lists all notices. If 'pebble ack' has been executed, this
will only list the notices that have occurred (or been repeated) since the
last one listed before the 'pebble ack'.

Timestamps are saved in a JSON file at $` + noticesFilenameEnvKey + `, which
defaults to "` + noticesFilenameDefault + `", where <socket-path> is the
unix socket used for the API, with '/' replaced by '-'.
`

type cmdNotices struct {
	client *client.Client

	timeMixin
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
			"--type":    "Only list notices of this type (multiple allowed)",
			"--key":     "Only list notices with this key (multiple allowed)",
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

	state, err := loadNoticesState()
	if err != nil {
		return fmt.Errorf("cannot load notices state: %w", err)
	}
	options := client.NoticesOptions{
		Types: cmd.Type,
		Keys:  cmd.Key,
		After: state.LastAcked,
	}

	var notices []*client.Notice
	if cmd.Timeout != 0 {
		ctx := notifyContext(context.Background(), os.Interrupt)
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

	fmt.Fprintln(writer, "ID\tType\tKey\tFirst\tLast\tRepeated\tOccurrences")

	for _, notice := range notices {
		key := notice.Key
		if len(key) > 32 {
			// Truncate to 32 bytes with ellipsis in the middle
			key = key[:14] + "..." + key[len(key)-15:]
		}
		fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%s\t%d\n",
			notice.ID,
			notice.Type,
			key,
			cmd.fmtTime(notice.FirstOccurred),
			cmd.fmtTime(notice.LastOccurred),
			cmd.fmtTime(notice.LastRepeated),
			notice.Occurrences)
	}

	state.LastListed = notices[len(notices)-1].LastRepeated
	err = saveNoticesState(state)
	if err != nil {
		return fmt.Errorf("cannot save notices state: %w", err)
	}
	return nil
}

type noticesState struct {
	LastListed time.Time `json:"last-listed"`
	LastAcked  time.Time `json:"last-acked"`
}

func loadNoticesState() (*noticesState, error) {
	filename, err := noticesFilename()
	if err != nil {
		return nil, fmt.Errorf("cannot determine notices filename: %w", err)
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return &noticesState{}, nil
		}
		return nil, err
	}
	var state noticesState
	err = json.Unmarshal(data, &state)
	if err != nil {
		return nil, err
	}
	return &state, nil
}

func saveNoticesState(state *noticesState) error {
	filename, err := noticesFilename()
	if err != nil {
		return fmt.Errorf("cannot determine notices filename: %w", err)
	}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}

	// Ensure parent dir exists
	user, err := osutil.RealUser()
	if err != nil {
		return err
	}
	uid, gid, err := osutil.UidGid(user)
	if err != nil {
		return err
	}
	err = osutil.MkdirAllChown(filepath.Dir(filename), 0700, uid, gid)
	if err != nil {
		return err
	}

	// Try to write the data atomically
	af, err := osutil.NewAtomicFile(filename, 0600, 0, uid, gid)
	if err != nil {
		return err
	}
	defer af.Cancel() // Cancel after Commit is a no-op
	_, err = af.Write(data)
	if err != nil {
		return err
	}
	return af.Commit()
}

func noticesFilename() (string, error) {
	if filename := os.Getenv(noticesFilenameEnvKey); filename != "" {
		return filename, nil
	}
	user, err := osutil.RealUser()
	if err != nil {
		return "", err
	}
	filename := strings.ReplaceAll(noticesFilenameDefault, "~", user.HomeDir)
	_, socketPath := getEnvPaths()
	socketPath = strings.ReplaceAll(socketPath, "/", "-")
	filename = strings.ReplaceAll(filename, "<socket-path>", socketPath)
	return filename, nil
}
