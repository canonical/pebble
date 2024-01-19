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

	"github.com/canonical/pebble/internals/plan"
)

var CalculateNextBackoff = calculateNextBackoff
var GetAction = getAction

func (m *ServiceManager) Plan() *plan.Plan {
	return m.plan()
}

func (m *ServiceManager) RunningCmds() map[string]*exec.Cmd {
	m.servicesLock.Lock()
	defer m.servicesLock.Unlock()

	cmds := make(map[string]*exec.Cmd)
	for name, s := range m.services {
		if s.state == stateRunning {
			cmds[name] = s.cmd
		}
	}
	return cmds
}

func (m *ServiceManager) BackoffNum(serviceName string) int {
	m.servicesLock.Lock()
	defer m.servicesLock.Unlock()

	s := m.services[serviceName]
	if s == nil {
		return -1
	}
	return s.backoffNum
}

func (m *ServiceManager) Config(serviceName string) *plan.Service {
	m.servicesLock.Lock()
	defer m.servicesLock.Unlock()

	s := m.services[serviceName]
	if s == nil {
		return nil
	}
	return s.config
}

func (m *ServiceManager) GetJitter(duration time.Duration) time.Duration {
	return m.getJitter(duration)
}

func FakeOkayWait(wait time.Duration) (restore func()) {
	old := okayDelay
	okayDelay = wait
	return func() {
		okayDelay = old
	}
}

// FakeKillFailDelay changes both the killDelayDefault and failDelay
// respectively for testing purposes.
func FakeKillFailDelay(newKillDelay, newFailDelay time.Duration) (restore func()) {
	old1, old2 := killDelayDefault, failDelay
	killDelayDefault, failDelay = newKillDelay, newFailDelay
	return func() {
		killDelayDefault, failDelay = old1, old2
	}
}

func FakeSetCmdCredential(f func(cmd *exec.Cmd, credential *syscall.Credential)) (restore func()) {
	old := setCmdCredential
	setCmdCredential = f
	return func() {
		setCmdCredential = old
	}
}
