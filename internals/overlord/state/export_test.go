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

package state

import (
	"time"
)

// FakeCheckpointRetryDelay changes unlockCheckpointRetryInterval and unlockCheckpointRetryMaxTime.
func FakeCheckpointRetryDelay(retryInterval, retryMaxTime time.Duration) (restore func()) {
	oldInterval := unlockCheckpointRetryInterval
	oldMaxTime := unlockCheckpointRetryMaxTime
	unlockCheckpointRetryInterval = retryInterval
	unlockCheckpointRetryMaxTime = retryMaxTime
	return func() {
		unlockCheckpointRetryInterval = oldInterval
		unlockCheckpointRetryMaxTime = oldMaxTime
	}
}

func FakeChangeTimes(chg *Change, spawnTime, readyTime time.Time) {
	chg.spawnTime = spawnTime
	chg.readyTime = readyTime
}

func FakeTaskTimes(t *Task, spawnTime, readyTime time.Time) {
	t.spawnTime = spawnTime
	t.readyTime = readyTime
}

func (t *Task) AccumulateDoingTime(duration time.Duration) {
	t.accumulateDoingTime(duration)
}

func (t *Task) AccumulateUndoingTime(duration time.Duration) {
	t.accumulateUndoingTime(duration)
}

// NumNotices returns the total number of notices, including expired ones that
// haven't yet been pruned.
func (s *State) NumNotices() int {
	return len(s.notices)
}
