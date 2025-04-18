// Copyright (c) 2025 Canonical Ltd
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

package tlsstate

import (
	"time"
)

// FakeSystemTime allows faking the time for the TLS manager.
func (m *TLSManager) FakeSystemTime(date string, offset time.Duration) (restore func(), clock time.Time) {
	layout := "2006-01-02"
	now, err := time.Parse(layout, date)
	if err != nil {
		panic("invalid date string")
	}
	now = now.Add(offset)
	old := systemTime
	systemTime = func() time.Time {
		return now
	}
	return func() {
		systemTime = old
	}, now
}
