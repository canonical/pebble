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

package state

import (
	"errors"
)

// marshalCertIdentity is not supported in FIPS builds.
// This should never be called since cert identities cannot be created in FIPS mode.
func marshalCertIdentity(cert *CertIdentity) *marshalledCertIdentity {
	// This should not happen in FIPS builds, but return empty if somehow reached
	return &marshalledCertIdentity{}
}

// unmarshalCertIdentity is not supported in FIPS builds.
// Returns an error if certificate data is encountered during state deserialization.
func unmarshalCertIdentity(mi *marshalledCertIdentity) (*CertIdentity, error) {
	return nil, errors.New("certificate identities are not supported in FIPS builds")
}
