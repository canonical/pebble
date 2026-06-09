// Copyright (c) 2026 Canonical Ltd
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

package cmdstate_test

import (
	"context"
	"net/http"
	"net/http/httptest"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/overlord/cmdstate"
	"github.com/canonical/pebble/internals/overlord/state"
)

type managerSuite struct{}

var _ = Suite(&managerSuite{})

// TestConnectContextCancelledGoroutineLeak demonstrates the goroutine leak in
// Connect. executionCh is unbuffered: if Connect exits via r.Context().Done()
// after waitExecution has already found a non-nil execution, the goroutine
// blocks forever trying to send on executionCh with no receiver.
//
// The test is probabilistic.
func (s *managerSuite) TestConnectContextCancelledGoroutineLeak(c *C) {
	st := state.New(nil)
	runner := state.NewTaskRunner(st)
	mgr := cmdstate.NewManager(runner)

	st.Lock()
	task := st.NewTask("exec", "test cmd")
	chg := st.NewChange("exec", "test change")
	chg.AddTask(task)
	st.Unlock()

	// Pre-register the execution so waitExecution returns immediately
	// with a non-nil value, before stopWait can be closed.
	mgr.AddTestExecution(task.ID())

	// Pre-cancel the context so Connect can exit via r.Context().Done()
	// while the goroutine is still trying to send on executionCh.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	r := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
	_ = mgr.Connect(r, httptest.NewRecorder(), task, "stdio")
}
