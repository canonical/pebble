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
	"gopkg.in/yaml.v3"

	"github.com/canonical/pebble/client"
)

const cmdNoticeSummary = "Fetch a single notice"
const cmdNoticeDescription = `
The notice command fetches a single notice, either by ID (1-arg variant), or
by unique type and key combination (2-arg variant).
`

type cmdNotice struct {
	client *client.Client

	formatMixin
	UID *uint32 `long:"uid"`

	Positional struct {
		IDOrType string `positional-arg-name:"<id-or-type>" required:"1"`
		Key      string `positional-arg-name:"<key>"`
	} `positional-args:"yes"`
}

func init() {
	AddCommand(&CmdInfo{
		Name:        "notice",
		Summary:     cmdNoticeSummary,
		Description: cmdNoticeDescription,

		ArgsHelp: merge(formatArgsHelp, map[string]string{
			"--uid": `Look up notice from user with this UID (admin only; 2-arg variant only)`,
		}),
		New: func(opts *CmdOptions) flags.Commander {
			return &cmdNotice{client: opts.Client}
		},
	})
}

func (cmd *cmdNotice) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	var notice *client.Notice
	if cmd.Positional.Key != "" {
		options := client.NoticesOptions{
			UserID: cmd.UID,
			Types:  []client.NoticeType{client.NoticeType(cmd.Positional.IDOrType)},
			Keys:   []string{cmd.Positional.Key},
		}
		notices, err := cmd.client.Notices(&options)
		if err != nil {
			return err
		}
		switch len(notices) {
		case 0:
			return fmt.Errorf("cannot find %s notice with key %q", cmd.Positional.IDOrType, cmd.Positional.Key)
		case 1:
			notice = notices[0]
		default:
			notice = notices[0]
			for _, n := range notices[1:] {
				if n.UserID != nil {
					// Should only ever be at most one notice retrieved with non-nil userID
					notice = n
					break
				}
			}
		}
	} else {
		if cmd.UID != nil {
			return fmt.Errorf("cannot use --uid option when looking up notice by key")
		}
		var err error
		notice, err = cmd.client.Notice(cmd.Positional.IDOrType)
		if err != nil {
			return err
		}
	}

	if cmd.Format == "text" {
		return cmd.writeText(notice)
	}

	return cmd.formatNonText(notice)
}

func (cmd *cmdNotice) writeText(notice *client.Notice) error {
	b, err := yaml.Marshal(notice)
	if err != nil {
		return err
	}
	fmt.Fprint(Stdout, string(b)) // yaml.Marshal includes the trailing newline
	return nil
}
