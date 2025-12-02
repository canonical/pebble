//go:build fips

// Copyright (c) 2025 Canonical Ltd
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

// idkey supplies an identity key for a machine, container or device.
// This is a stub implementation for FIPS builds where crypto operations are not supported.
package idkey

import (
	"crypto"
	"errors"
	"io"
)

// IDKey is a stub for FIPS builds.
type IDKey struct{}

// Get is not supported in FIPS builds.
func Get(keyDir string) (*IDKey, error) {
	return nil, errors.New("identity key operations are not supported in FIPS builds")
}

// Generate is not supported in FIPS builds.
func Generate(keyDir string) (*IDKey, error) {
	return nil, errors.New("identity key generation is not supported in FIPS builds")
}

// Load is not supported in FIPS builds.
func Load(keyDir string) (*IDKey, error) {
	return nil, errors.New("identity key loading is not supported in FIPS builds")
}

// Public returns nil for FIPS builds.
func (k *IDKey) Public() crypto.PublicKey {
	return nil
}

// Sign is not supported in FIPS builds.
func (k *IDKey) Sign(rand io.Reader, digest []byte, opts crypto.SignerOpts) (signature []byte, err error) {
	return nil, errors.New("signing operations are not supported in FIPS builds")
}

// Fingerprint is not supported in FIPS builds.
func (k *IDKey) Fingerprint() string {
	return ""
}
