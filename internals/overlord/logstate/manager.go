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
	"github.com/canonical/pebble/internals/plan"
	"github.com/canonical/pebble/internals/servicelog"
)

type LogManager struct{}

func NewLogManager() *LogManager {
	return &LogManager{}
}

// PlanChanged is called by the service manager when the plan changes. We stop
// all running forwarders, and start new forwarders based on the new plan.
func (m *LogManager) PlanChanged(pl *plan.Plan) {
	// TODO: implement
}

// ServiceStarted notifies the log manager that the named service has started,
// and provides a reference to the service's log buffer.
func (m *LogManager) ServiceStarted(serviceName string, buffer *servicelog.RingBuffer) {
	// TODO: implement
}

// DryStart implements overlord.StateManager.
func (m *LogManager) DryStart() error {
	return nil
}

// Ensure implements overlord.StateManager.
func (m *LogManager) Ensure() error {
	return nil
}

// Stop implements overlord.StateStopper and stops all log forwarding.
func (m *LogManager) Stop() {
	// TODO: implement
}
