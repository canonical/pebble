// Copyright (c) 2022 Canonical Ltd
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
//go:build !linux

package reaper

// setChildSubreaper is a no-op on non-Linux systems.
//
// Subreaping only matters when Pebble is not PID 1 and wants orphaned
// descendants of its services reparented to it rather than to init. The
// reaper only registers direct children (see StartCommand), which wait4(-1)
// still reaps without subreaping, and orphaned grandchildren are reaped by
// init. So nothing is lost by skipping it on systems that lack the facility
// or where Pebble never runs as PID 1.
func setChildSubreaper(set int) error {
	return nil
}
