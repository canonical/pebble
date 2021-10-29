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

package servstate

import (
	"os/exec"
	"syscall"
	"time"
)

func (m *ServiceManager) CmdsForTest() map[string]*exec.Cmd {
	m.servicesLock.Lock()
	defer m.servicesLock.Unlock()

	cmds := make(map[string]*exec.Cmd)
	for name, active := range m.services {
		cmds[name] = active.cmd
	}
	return cmds
}

func FakeOkayWait(wait time.Duration) (restore func()) {
	old := okayWait
	okayWait = wait
	return func() {
		okayWait = old
	}
}

func FakeKillWait(kill, fail time.Duration) (restore func()) {
	old1, old2 := killWait, failWait
	killWait, failWait = kill, fail
	return func() {
		killWait, failWait = old1, old2
	}
}

func FakeSetCmdCredential(f func(cmd *exec.Cmd, credential *syscall.Credential)) (restore func()) {
	old := setCmdCredential
	setCmdCredential = f
	return func() {
		setCmdCredential = old
	}
}
