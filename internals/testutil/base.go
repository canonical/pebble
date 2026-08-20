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

package testutil

import (
	"gopkg.in/check.v1"
)

// BaseTest is a structure used as a base test suite for many of the pebble
// tests.
type BaseTest struct {
	cleanupHandlers []func()
}

// SetUpTest prepares the cleanup
func (s *BaseTest) SetUpTest(c *check.C) {
	s.cleanupHandlers = nil
}

// TearDownTest cleans up the channel.ini files in case they were changed by
// the test.
// It also runs the cleanup handlers
func (s *BaseTest) TearDownTest(c *check.C) {
	// run cleanup handlers and clear the slice
	for _, f := range s.cleanupHandlers {
		f()
	}
	s.cleanupHandlers = nil
}

// AddCleanup adds a new cleanup function to the test
func (s *BaseTest) AddCleanup(f func()) {
	s.cleanupHandlers = append(s.cleanupHandlers, f)
}

// Backup a single element before further mocking.
func Backup[T any](mockable *T) (restore func()) {
	backup := *mockable

	return func() {
		*mockable = backup
	}
}

// Fake provides a type safe way of faking a single thing.
func Fake[T any](fakeable *T, newVal T) (restore func()) {
	restore = Backup(fakeable)
	*fakeable = newVal
	return restore
}
