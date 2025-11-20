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

	"github.com/canonical/pebble/internals/osutil"
)

type overlordStateBackend struct {
	path         string
	ensureBefore func(d time.Duration)
}

func (osb *overlordStateBackend) Checkpoint(data []byte) error {
	return osutil.AtomicWriteFile(osb.path, data, 0o600, 0)
}

func (osb *overlordStateBackend) EnsureBefore(d time.Duration) {
	osb.ensureBefore(d)
}

func (osb *overlordStateBackend) NeedsCheckpoint() bool {
	return true
}

type inMemoryBackend struct {
	ensureBefore func(d time.Duration)
}

// Checkpoint of the in-memory backend does nothing because
// we're keeping the state in memory only.
func (imb *inMemoryBackend) Checkpoint(data []byte) error {
	panic("internal error: inMemoryBackend.Checkpoint should never be called")
}

func (imb *inMemoryBackend) EnsureBefore(d time.Duration) {
	imb.ensureBefore(d)
}

func (imb *inMemoryBackend) NeedsCheckpoint() bool {
	return false
}
