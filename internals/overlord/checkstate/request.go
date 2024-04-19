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
	Name     string // name of check
	Failures int    // failure count
	Proceed  bool   // whether to proceed to next check type when change is ready
}

func performCheck(st *state.State, checkName, checkType string) *state.Task {
	task := st.NewTask(performCheckKind, fmt.Sprintf("Perform %s check %q", checkType, checkName))
	task.Set(checkDetailsAttr, &checkDetails{Name: checkName})
	return task
}

func performCheckChange(st *state.State, config *plan.Check) string {
	task := performCheck(st, config.Name, checkType(config))
	change := st.NewChange(performCheckKind, task.Summary())
	change.Set(noPruneAttr, true)
	change.AddTask(task)
	return change.ID()
}

func recoverCheck(st *state.State, checkName, checkType string, failures int) *state.Task {
	task := st.NewTask(recoverCheckKind, fmt.Sprintf("Recover %s check %q", checkType, checkName))
	task.Set(checkDetailsAttr, &checkDetails{Name: checkName, Failures: failures})
	return task
}

func recoverCheckChange(st *state.State, config *plan.Check, failures int) string {
	task := recoverCheck(st, config.Name, checkType(config), failures)
	change := st.NewChange(recoverCheckKind, task.Summary())
	change.Set(noPruneAttr, true)
	change.AddTask(task)
	return change.ID()
}
