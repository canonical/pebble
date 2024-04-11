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
)

type checkDetails struct {
	Name string
}

func performCheck(st *state.State, checkName, checkType string) *state.Task {
	task := st.NewTask("perform-check", fmt.Sprintf("Perform %s check %q", checkType, checkName))
	task.Set("check-details", &checkDetails{Name: checkName})
	return task
}

func recoverCheck(st *state.State, checkName, checkType string) *state.Task {
	task := st.NewTask("recover-check", fmt.Sprintf("Recover %s check %q", checkType, checkName))
	task.Set("check-details", &checkDetails{Name: checkName})
	return task
}
