//go:build fips

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

package cli

import (
	"github.com/canonical/pebble/internals/overlord/tlsstate"
)

// getIDSigner returns nil in FIPS builds (identity keys not supported).
// This is safe because HTTPS (which requires IDSigner) is blocked in FIPS mode.
func getIDSigner(pebbleDir string) (tlsstate.IDSigner, error) {
	return nil, nil
}
