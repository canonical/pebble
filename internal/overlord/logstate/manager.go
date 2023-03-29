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
	"github.com/canonical/pebble/internal/logger"
	"github.com/canonical/pebble/internal/plan"
	"github.com/canonical/pebble/internal/servicelog"
)

type LogManager struct{}

func NewLogManager() *LogManager {
	return &LogManager{}
}

// PlanChanged is called by the service manager when the plan changes. We stop
// all running forwarders, and start new forwarders based on the new plan.
func (m *LogManager) PlanChanged(pl *plan.Plan) {
	logger.Debugf("LogManager.PlanChanged called")
	targets := selectTargets(pl)
	logger.Debugf("LogManager: selected the following targets:")
	for serviceName, targetMap := range targets {
		var targetNames []string
		for targetName := range targetMap {
			targetNames = append(targetNames, targetName)
		}
		logger.Debugf("- %s -> %v", serviceName, targetNames)
	}
}

// selectTargets goes through the given plan and determines which service's
// logs should be forwarded to which log targets.
func selectTargets(p *plan.Plan) map[string]map[string]*plan.LogTarget {
	planTargets := make(map[string]map[string]*plan.LogTarget, len(p.Services))

	for serviceName, service := range p.Services {
		planTargets[serviceName] = make(map[string]*plan.LogTarget, len(service.LogTargets))

		for _, targetName := range service.LogTargets {
			target, ok := p.LogTargets[targetName]
			if !ok {
				logger.Noticef("Internal error: log manager cannot find target %q (specified by service %q in plan)",
					targetName, serviceName)
				continue
			}
			if target.Selection != plan.DisabledSelection {
				planTargets[serviceName][targetName] = target
			}
		}

		// Add opt-out targets if none specified
		if len(service.LogTargets) == 0 {
			for targetName, target := range p.LogTargets {
				// Default (unset) is opt-out
				if target.Selection == plan.OptOutSelection || target.Selection == plan.UnsetSelection {
					planTargets[serviceName][targetName] = target
				}
			}
		}
	}

	return planTargets
}

// ServiceStarted notifies the log manager that the named service has started,
// and provides a reference to the service's log buffer.
func (m *LogManager) ServiceStarted(serviceName string, buffer *servicelog.RingBuffer) {
	logger.Debugf("LogManager.ServiceStarted called for service %q", serviceName)
}

// Ensure implements overlord.StateManager.
func (m *LogManager) Ensure() error {
	return nil
}

// Stop implements overlord.StateStopper and stops all log forwarding.
func (m *LogManager) Stop() {
	logger.Debugf("LogManager.Stop called")
}
