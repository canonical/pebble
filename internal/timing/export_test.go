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

package timing

import (
	"time"
)

func FakeTimeNow(nowFunc func() time.Time) func() {
	old := timeNow
	timeNow = nowFunc
	return func() {
		timeNow = old
	}
}

func FakeSpanDuration(durationFunc func(a, b uint64) time.Duration) func() {
	old := spanDuration
	spanDuration = durationFunc
	return func() {
		spanDuration = old
	}
}
