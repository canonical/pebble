// Copyright (c) 2026 Canonical Ltd
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

//go:build linux

package httputil

// LoadSystemRoots exposes loadSystemRoots for conformance testing.
var LoadSystemRoots = loadSystemRoots

// Initialised reports whether the Transport's underlying http.Transport
// has been created yet (i.e. whether lazyInit has run).
func (t *Transport) Initialised() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.transport != nil
}
