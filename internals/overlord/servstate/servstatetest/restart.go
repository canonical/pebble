// Copyright (c) 2014-2021 Canonical Ltd
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

package servstatetest

import (
	"github.com/canonical/pebble/internals/overlord/restart"
	"github.com/canonical/pebble/internals/overlord/state"
)

// FakeRestartHandler fakes a restart.Handler based on a function
// to witness the restart requests.
type FakeRestartHandler func(restart.RestartType)

func (h FakeRestartHandler) HandleRestart(t restart.RestartType) {
	if h == nil {
		return
	}
	h(t)
}

func (h FakeRestartHandler) RebootIsFine(*state.State) error {
	return nil
}

func (h FakeRestartHandler) RebootIsMissing(*state.State) error {
	panic("internal error: fakeing should not invoke RebootIsMissing")
}
