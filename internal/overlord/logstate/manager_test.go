// Copyright (c) 2023 Canonical Ltd
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

package logstate

import (
	"bytes"
	"testing"

	"github.com/canonical/pebble/internal/logger"
	"github.com/canonical/pebble/internal/plan"
	. "gopkg.in/check.v1"
)

type managerSuite struct {
	logbuf        *bytes.Buffer
	restoreLogger func()
}

var _ = Suite(&managerSuite{})

func Test(t *testing.T) { TestingT(t) }

func (s *managerSuite) SetUpTest(c *C) {
	s.logbuf, s.restoreLogger = logger.MockLogger("PREFIX: ")
}

func (s *managerSuite) TearDownTest(c *C) {
	s.restoreLogger()
}

func (s *managerSuite) TestSelectTargets(c *C) {
	unset := plan.LogTarget{Selection: plan.UnsetSelection}
	optout := plan.LogTarget{Selection: plan.OptOutSelection}
	optin := plan.LogTarget{Selection: plan.OptInSelection}
	disabled := plan.LogTarget{Selection: plan.DisabledSelection}

	input := plan.Plan{
		LogTargets: map[string]*plan.LogTarget{
			"unset":    &unset,
			"optout":   &optout,
			"optin":    &optin,
			"disabled": &disabled,
		},
		Services: map[string]*plan.Service{
			"svc1": {LogTargets: nil},
			"svc2": {LogTargets: []string{}},
			"svc3": {LogTargets: []string{"unset"}},
			"svc4": {LogTargets: []string{"optout"}},
			"svc5": {LogTargets: []string{"optin"}},
			"svc6": {LogTargets: []string{"disabled"}},
			"svc7": {LogTargets: []string{"unset", "optin", "disabled"}},
		},
	}

	expected := map[string]map[string]*plan.LogTarget{
		"svc1": {"unset": &unset, "optout": &optout},
		"svc2": {"unset": &unset, "optout": &optout},
		"svc3": {"unset": &unset},
		"svc4": {"optout": &optout},
		"svc5": {"optin": &optin},
		"svc6": {},
		"svc7": {"unset": &unset, "optin": &optin},
	}

	planTargets := selectTargets(&input)
	c.Check(planTargets, DeepEquals, expected)
	// Check no error messages were logged
	c.Check(s.logbuf.Bytes(), HasLen, 0)
}
