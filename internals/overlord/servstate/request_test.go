// Copyright (c) 2014-2024 Canonical Ltd
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

package servstate_test

import (
	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/overlord/servstate"
)

func (s *S) TestStart(c *C) {
	s.newServiceManager(c)
	layer := `
services:
    one:
        override: replace
        command: /bin/sh -c "echo one; sleep 10"
        startup: enabled

    two:
        override: replace
        command: /bin/sh -c "echo two; sleep 10"
        startup: enabled
`
	s.planAddLayer(c, layer)
	s.planChanged(c)

	s.st.Lock()
	defer s.st.Unlock()

	tset, err := servstate.Start(s.st, [][]string{{"one"}, {"two"}})
	c.Assert(err, IsNil)

	tasks := tset.Tasks()
	c.Assert(len(tasks), Equals, 2)

	c.Assert(tasks[0].Kind(), Equals, "start")
	req, err := servstate.TaskServiceRequest(tasks[0])
	c.Assert(err, IsNil)
	c.Assert(req.Name, Equals, "one")

	c.Assert(tasks[1].Kind(), Equals, "start")
	req, err = servstate.TaskServiceRequest(tasks[1])
	c.Assert(err, IsNil)
	c.Assert(req.Name, Equals, "two")

	c.Assert(tasks[0].Lanes()[0], Not(Equals), tasks[1].Lanes()[0])
}

func (s *S) TestStartInTheSameLaneAfter(c *C) {
	s.newServiceManager(c)
	layer := `
services:
    one:
        override: replace
        command: /bin/sh -c "echo one; sleep 10"
        startup: enabled
        requires:
            - two

    two:
        override: replace
        command: /bin/sh -c "echo two; sleep 10"
        startup: enabled
        after:
            - one
`
	s.planAddLayer(c, layer)
	s.planChanged(c)

	s.st.Lock()
	defer s.st.Unlock()

	tset, err := servstate.Start(s.st, [][]string{{"one", "two"}})
	c.Assert(err, IsNil)

	tasks := tset.Tasks()
	c.Assert(len(tasks), Equals, 2)

	c.Assert(tasks[0].Kind(), Equals, "start")
	req, err := servstate.TaskServiceRequest(tasks[0])
	c.Assert(err, IsNil)
	c.Assert(req.Name, Equals, "one")

	c.Assert(tasks[1].Kind(), Equals, "start")
	req, err = servstate.TaskServiceRequest(tasks[1])
	c.Assert(err, IsNil)
	c.Assert(req.Name, Equals, "two")

	c.Assert(tasks[0].Lanes()[0], Equals, tasks[1].Lanes()[0])
}

func (s *S) TestStartInTheSameLaneBefore(c *C) {
	s.newServiceManager(c)
	layer := `
services:
    one:
        override: replace
        command: /bin/sh -c "echo one; sleep 10"
        startup: enabled
        requires:
            - two
        before:
            - two

    two:
        override: replace
        command: /bin/sh -c "echo two; sleep 10"
        startup: enabled
`
	s.planAddLayer(c, layer)
	s.planChanged(c)

	s.st.Lock()
	defer s.st.Unlock()

	tset, err := servstate.Start(s.st, [][]string{{"one", "two"}})
	c.Assert(err, IsNil)

	tasks := tset.Tasks()
	c.Assert(len(tasks), Equals, 2)

	c.Assert(tasks[0].Kind(), Equals, "start")
	req, err := servstate.TaskServiceRequest(tasks[0])
	c.Assert(err, IsNil)
	c.Assert(req.Name, Equals, "one")

	c.Assert(tasks[1].Kind(), Equals, "start")
	req, err = servstate.TaskServiceRequest(tasks[1])
	c.Assert(err, IsNil)
	c.Assert(req.Name, Equals, "two")

	c.Assert(tasks[0].Lanes()[0], Equals, tasks[1].Lanes()[0])
}

func (s *S) TestStop(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	tset, err := servstate.Stop(s.st, [][]string{{"one", "two"}})
	c.Assert(err, IsNil)

	tasks := tset.Tasks()
	c.Assert(len(tasks), Equals, 2)

	c.Assert(tasks[0].Kind(), Equals, "stop")
	req, err := servstate.TaskServiceRequest(tasks[0])
	c.Assert(err, IsNil)
	c.Assert(req.Name, Equals, "one")

	c.Assert(tasks[1].Kind(), Equals, "stop")
	req, err = servstate.TaskServiceRequest(tasks[1])
	c.Assert(err, IsNil)
	c.Assert(req.Name, Equals, "two")
}
