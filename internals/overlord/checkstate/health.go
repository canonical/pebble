// Copyright (c) 2024 Canonical Ltd
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

package checkstate

import (
	"sort"

	"github.com/canonical/pebble/internals/plan"
)

// HealthInfo provides basic health information about a check.
type HealthInfo struct {
	Name   string
	Level  plan.CheckLevel
	Status CheckStatus
}

// Health returns basic health information about the currently-configured checks,
// ordered by name.
//
// NOTE: this is similar to Checks(), but doesn't provide as much information,
// and doesn't acquire the state lock, which is important for the /v1/health
// endpoint.
func (m *CheckManager) Health() ([]*HealthInfo, error) {
	m.healthLock.Lock()
	defer m.healthLock.Unlock()

	infos := make([]*HealthInfo, 0, len(m.health))
	for _, h := range m.health {
		infos = append(infos, &h)
	}
	sort.Slice(infos, func(i, j int) bool {
		return infos[i].Name < infos[j].Name
	})
	return infos, nil
}

func (m *CheckManager) updateHealthInfo(config *plan.Check, failures int) {
	m.healthLock.Lock()
	defer m.healthLock.Unlock()

	status := CheckStatusUp
	if failures >= config.Threshold {
		status = CheckStatusDown
	}
	m.health[config.Name] = HealthInfo{
		Name:   config.Name,
		Level:  config.Level,
		Status: status,
	}
}

func (m *CheckManager) deleteHealthInfo(name string) {
	m.healthLock.Lock()
	defer m.healthLock.Unlock()

	delete(m.health, name)
}
