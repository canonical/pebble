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

package overlord

import (
	"time"
)

// FakeEnsureInterval sets the overlord ensure interval for tests.
func FakeEnsureInterval(d time.Duration) (restore func()) {
	old := ensureInterval
	ensureInterval = d
	return func() { ensureInterval = old }
}

// FakePruneInterval sets the overlord prune interval for tests.
func FakePruneInterval(prunei, prunew, abortw time.Duration) (restore func()) {
	oldPruneInterval := pruneInterval
	oldPruneWait := pruneWait
	oldAbortWait := abortWait
	pruneInterval = prunei
	pruneWait = prunew
	abortWait = abortw
	return func() {
		pruneInterval = oldPruneInterval
		pruneWait = oldPruneWait
		abortWait = oldAbortWait
	}
}

func FakePruneTicker(f func(t *time.Ticker) <-chan time.Time) (restore func()) {
	old := pruneTickerC
	pruneTickerC = f
	return func() {
		pruneTickerC = old
	}
}

// FakeEnsureNext sets o.ensureNext for tests.
func FakeEnsureNext(o *Overlord, t time.Time) {
	o.ensureNext = t
}

// Engine exposes the state engine in an Overlord for tests.
func (o *Overlord) Engine() *StateEngine {
	return o.stateEng
}

func FakeTimeNow(f func() time.Time) (restore func()) {
	old := timeNow
	timeNow = f
	return func() {
		timeNow = old
	}
}
