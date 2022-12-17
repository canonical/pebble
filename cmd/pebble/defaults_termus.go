//go:build termus
// +build termus

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

package main

// defaultPebbleDir is the Pebble directory used if $PEBBLE is not set. It is
// created by the daemon ("pebble run") if it doesn't exist, and also used by
// the pebble client.
const defaultPebbleDir = "/termus/var/lib/pebble"
