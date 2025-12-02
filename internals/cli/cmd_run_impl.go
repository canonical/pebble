//go:build !fips

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
	"path/filepath"

	"github.com/canonical/pebble/internals/idkey"
	"github.com/canonical/pebble/internals/overlord/tlsstate"
)

// getIDSigner loads the identity key for signing TLS certificates.
func getIDSigner(pebbleDir string) (tlsstate.IDSigner, error) {
	idPath := filepath.Join(pebbleDir, "identity")
	return idkey.Get(idPath)
}
