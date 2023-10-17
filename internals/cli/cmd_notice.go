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
	"time"

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
		ArgsHelp:    map[string]string{},
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
		notices, err := cmd.client.Notices(&client.NoticesOptions{
			Types: []client.NoticeType{client.NoticeType(cmd.Positional.IDOrType)},
			Keys:  []string{cmd.Positional.Key},
		})
		if err != nil {
			return err
		}
		if len(notices) == 0 {
			return fmt.Errorf("cannot find %s notice with key %q", cmd.Positional.IDOrType, cmd.Positional.Key)
		}
		notice = notices[0]
	} else {
		var err error
		notice, err = cmd.client.Notice(cmd.Positional.IDOrType)
		if err != nil {
			return err
		}
	}

	// Notice can be assigned directly to yamlNotice as only the tags are different.
	yn := yamlNotice(*notice)

	b, err := yaml.Marshal(yn)
	if err != nil {
		return err
	}
	fmt.Fprint(Stdout, string(b)) // yaml.Marshal includes the trailing newline
	return nil
}

// yamlNotice exists to add "yaml" tags to the Notice fields.
type yamlNotice struct {
	ID            string            `yaml:"id"`
	Type          client.NoticeType `yaml:"type"`
	Key           string            `yaml:"key"`
	FirstOccurred time.Time         `yaml:"first-occurred"`
	LastOccurred  time.Time         `yaml:"last-occurred"`
	LastRepeated  time.Time         `yaml:"last-repeated"`
	Occurrences   int               `yaml:"occurrences"`
	LastData      map[string]string `yaml:"last-data,omitempty"`
	RepeatAfter   time.Duration     `yaml:"repeat-after,omitempty"`
	ExpireAfter   time.Duration     `yaml:"expire-after,omitempty"`
}
