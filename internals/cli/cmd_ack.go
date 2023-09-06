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

	"github.com/canonical/go-flags"
)

const cmdAckSummary = "Acknowledge notices most recently listed"
const cmdAckDescription = `
The ack command acknowledges notices up to most recent notice listed in the
last 'pebble notices' invocation. After execution, 'pebble notices' will only
list the notices that have occurred (or been repeated) more recently.

Timestamps are saved in a JSON file at $` + noticesFilenameEnvKey + `, which
defaults to "` + noticesFilenameDefault + `", where <socket-path> is the
unix socket used for the API, with '/' replaced by '-'.
`

type cmdAck struct{}

func init() {
	AddCommand(&CmdInfo{
		Name:        "ack",
		Summary:     cmdAckSummary,
		Description: cmdAckDescription,
		New: func(opts *CmdOptions) flags.Commander {
			return &cmdAck{}
		},
	})
}

func (cmd *cmdAck) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	state, err := loadNoticesState()
	if err != nil {
		return fmt.Errorf("cannot load notices state: %w", err)
	}
	if state.LastListed.IsZero() {
		return fmt.Errorf("no notices have been listed; try 'pebble notices'")
	}
	state.LastAcked = state.LastListed
	err = saveNoticesState(state)
	if err != nil {
		return fmt.Errorf("cannot save notices state: %w", err)
	}
	return nil
}
