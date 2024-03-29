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

package overlord

import (
	"gopkg.in/tomb.v2"

	"github.com/canonical/pebble/internals/overlord/state"
)

func handleUnknownTask(task *state.Task, tomb *tomb.Tomb) error {
	st := task.State()
	st.Lock()
	defer st.Unlock()
	task.Logf("no handler for task %q, task ignored", task.Kind())
	return nil
}
