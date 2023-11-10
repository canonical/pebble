// Copyright (c) 2014-2020 Canonical Ltd
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
	"errors"
	"fmt"

	"github.com/canonical/pebble/client"
)

type changeIDMixin struct {
	LastChangeType string `long:"last"`
	Positional     struct {
		ChangeID string `positional-arg-name:"<change-id>"`
	} `positional-args:"yes"`
}

var changeIDMixinArgsHelp = map[string]string{
	"<change-id>": "Change ID",
	"--last":      "Select last change of given type (install, refresh, remove, try, auto-refresh, etc.). A question mark at the end of the type means to do nothing (instead of returning an error) if no change of the given type is found. Note the question mark could need protecting from the shell.",
}

// should not be user-visible, but keep it clear and polite because mistakes happen
var noChangeFoundOK = errors.New("no change found but that's ok")

func (l *changeIDMixin) GetChangeID(cli *client.Client) (string, error) {
	if l.Positional.ChangeID == "" && l.LastChangeType == "" {
		return "", fmt.Errorf("please provide change ID or type with --last=<type>")
	}

	if l.Positional.ChangeID != "" {
		if l.LastChangeType != "" {
			return "", fmt.Errorf("cannot use change ID and type together")
		}

		return string(l.Positional.ChangeID), nil
	}

	// note that at this point we know l.LastChangeType != ""
	kind := l.LastChangeType
	optional := false
	if l := len(kind) - 1; kind[l] == '?' {
		optional = true
		kind = kind[:l]
	}
	changes, err := queryChanges(cli, &client.ChangesOptions{Selector: client.ChangesAll})
	if err != nil {
		return "", err
	}
	if len(changes) == 0 {
		if optional {
			return "", noChangeFoundOK
		}
		return "", fmt.Errorf("no changes found")
	}
	chg := findLatestChangeByKind(changes, kind)
	if chg == nil {
		if optional {
			return "", noChangeFoundOK
		}
		return "", fmt.Errorf("no changes of type %q found", l.LastChangeType)
	}

	return chg.ID, nil
}

func findLatestChangeByKind(changes []*client.Change, kind string) (latest *client.Change) {
	for _, chg := range changes {
		if chg.Kind == kind && (latest == nil || latest.SpawnTime.Before(chg.SpawnTime)) {
			latest = chg
		}
	}
	return latest
}
