// Copyright (c) 2024 Canonical Ltd
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

package checkstate

import (
	"fmt"

	"github.com/canonical/pebble/internals/overlord/state"
	"github.com/canonical/pebble/internals/plan"
)

type checkDetails struct {
	Name     string `json:"name"`
	Failures int    `json:"failures"`
	// Whether to proceed to next check type when change is ready
	Proceed bool `json:"proceed,omitempty"`
}

type performConfigKey struct {
	changeID string
}

func performCheckChange(st *state.State, config *plan.Check) (changeID string) {
	summary := fmt.Sprintf("Perform %s check %q", checkType(config), config.Name)
	task := st.NewTask(performCheckKind, summary)
	task.Set(checkDetailsAttr, &checkDetails{Name: config.Name})

	change := st.NewChangeWithNoticeData(performCheckKind, task.Summary(), map[string]string{
		"check-name": config.Name,
	})
	change.Set(noPruneAttr, true)
	change.AddTask(task)

	st.Cache(performConfigKey{change.ID()}, config)

	return change.ID()
}

type recoverConfigKey struct {
	changeID string
}

func recoverCheckChange(st *state.State, config *plan.Check, failures int) (changeID string) {
	summary := fmt.Sprintf("Recover %s check %q", checkType(config), config.Name)
	task := st.NewTask(recoverCheckKind, summary)
	task.Set(checkDetailsAttr, &checkDetails{Name: config.Name, Failures: failures})

	change := st.NewChangeWithNoticeData(recoverCheckKind, task.Summary(), map[string]string{
		"check-name": config.Name,
	})
	change.Set(noPruneAttr, true)
	change.AddTask(task)

	st.Cache(recoverConfigKey{change.ID()}, config)

	return change.ID()
}
