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
	"sync"
	"syscall"
	"time"
)

func (m *ServiceManager) RunningCmds() map[string]*exec.Cmd {
	m.servicesLock.Lock()
	defer m.servicesLock.Unlock()

	cmds := make(map[string]*exec.Cmd)
	for name, s := range m.services {
		s.lock.Lock()
		if s.state == stateRunning {
			cmds[name] = s.cmd
		}
		s.lock.Unlock()
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

func (m *ServiceManager) FakeTime() func(time.Duration, int) {
	ft := &fakeTime{}
	m.time = ft
	return ft.Advance
}

type fakeTime struct {
	lock    sync.Mutex
	current time.Duration
	funcs   []fakeTimeFunc
}

type fakeTimeFunc struct {
	end time.Duration
	f   func()
}

func (t *fakeTime) AfterFunc(d time.Duration, f func()) timer {
	t.lock.Lock()
	defer t.lock.Unlock()

	index := len(t.funcs)
	t.funcs = append(t.funcs, fakeTimeFunc{end: t.current + d, f: f})
	return &fakeTimer{time: t, index: index}
}

// TODO: make it remove stopped timers so nTimers is only the new ones
func (t *fakeTime) Advance(d time.Duration, nTimers int) {
	// First wait for at least nTimers timer funcs to be added.
	for i := 0; i < 100; i++ {
		t.lock.Lock()
		nAdded := len(t.funcs)
		t.lock.Unlock()
		if nAdded >= nTimers {
			break
		}
		time.Sleep(time.Millisecond)
	}

	t.lock.Lock()
	defer t.lock.Unlock()

	t.current += d

	for i, f := range t.funcs {
		if f.f == nil {
			continue
		}
		if t.current >= f.end {
			t.lock.Unlock()
			f.f()
			t.lock.Lock()
			t.funcs[i].f = nil
		}
	}
}

type fakeTimer struct {
	time  *fakeTime
	index int
}

func (t *fakeTimer) Stop() bool {
	t.time.lock.Lock()
	defer t.time.lock.Unlock()

	if t.time.funcs[t.index].f == nil {
		return false
	}
	t.time.funcs[t.index].f = nil
	return true
}
