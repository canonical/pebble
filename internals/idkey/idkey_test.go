//go:build !fips

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

package idkey_test

import (
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/idkey"
)

// TestNoDirectory checks if leaf directory creation works.
func (ks *keySuite) TestNoDirectory(c *C) {
	keyDir := filepath.Join(c.MkDir(), "identity")

	// Create a new identity key (first boot)
	firstBoot, err := idkey.Generate(keyDir)
	c.Assert(err, IsNil)

	// Load the identity key (other boots)
	nextBoot, err := idkey.Load(keyDir)
	c.Assert(err, IsNil)

	// Both should be the same identity.
	c.Assert(firstBoot.Fingerprint(), Equals, nextBoot.Fingerprint())
}

// TestGet checks if the Get() function correctly only creates a new
// identity the first time.
func (ks *keySuite) TestGet(c *C) {
	keyDir := filepath.Join(c.MkDir(), "identity")

	// Create a new identity key (first boot)
	firstBoot, err := idkey.Get(keyDir)
	c.Assert(err, IsNil)

	// Load the identity key (other boots)
	nextBoot, err := idkey.Get(keyDir)
	c.Assert(err, IsNil)

	// Both should be the same identity.
	c.Assert(firstBoot.Fingerprint(), Equals, nextBoot.Fingerprint())
}

// TestDirectoryInvalid confirms that if the leaf directory has invalid
// permissions we exit.
func (ks *keySuite) TestDirectoryInvalid(c *C) {
	keyDir := filepath.Join(c.MkDir(), "identity")
	err := os.MkdirAll(keyDir, 0o740)
	c.Assert(err, IsNil)

	// Loading
	_, err = idkey.Load(keyDir)
	c.Assert(err, ErrorMatches, ".* expected permission 0o700 .*")

	// Saving
	_, err = idkey.Generate(keyDir)
	c.Assert(err, ErrorMatches, ".* expected permission 0o700 .*")
}

// TestDirInvalid checks for a missing non-leaf directory. The caller
// must create this.
func (ks *keySuite) TestDirInvalid(c *C) {
	keyDir := filepath.Join(c.MkDir(), "foo/identity")

	// Saving
	_, err := idkey.Generate(keyDir)
	c.Assert(err, ErrorMatches, "cannot create identity directory.*")
}

// TestInvalidKey checks permission of the key file.
func (ks *keySuite) TestInvalidKey(c *C) {
	keyDir := filepath.Join(c.MkDir(), "identity")

	// Create a new identity key (first boot)
	_, err := idkey.Generate(keyDir)
	c.Assert(err, IsNil)

	err = os.Chmod(filepath.Join(keyDir, "key.pem"), 0o644)
	c.Assert(err, IsNil)

	// Load the identity key (other boots)
	_, err = idkey.Load(keyDir)
	c.Assert(err, ErrorMatches, ".*expected permission.*")
}

// TestEmptyKey checks if a key fails to load.
func (ks *keySuite) TestEmptyKey(c *C) {
	keyDir := filepath.Join(c.MkDir(), "identity")

	// Create a new identity key (first boot)
	_, err := idkey.Generate(keyDir)
	c.Assert(err, IsNil)

	// Zero the existing file.
	f, err := os.OpenFile(filepath.Join(keyDir, "key.pem"), os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600)
	c.Assert(err, IsNil)
	err = f.Close()
	c.Assert(err, IsNil)

	// Load the identity key (other boots)
	_, err = idkey.Load(keyDir)
	c.Assert(err, ErrorMatches, ".*missing 'PRIVATE KEY' block.*")
}

// TestKeyWithTrailingBytes checks if a key fails to load if unexpected
// bytes follow the private key block.
func (ks *keySuite) TestKeyWithTrailingBytes(c *C) {
	keyDir := filepath.Join(c.MkDir(), "identity")

	// Create a new identity key (first boot)
	_, err := idkey.Generate(keyDir)
	c.Assert(err, IsNil)

	// Append some unexpected bytes after the PEM block.
	f, err := os.OpenFile(filepath.Join(keyDir, "key.pem"), os.O_RDWR|os.O_APPEND, 0o600)
	c.Assert(err, IsNil)
	_, err = f.Write([]byte("\n1234567890"))
	c.Assert(err, IsNil)
	err = f.Close()
	c.Assert(err, IsNil)

	// Load the identity key (other boots)
	_, err = idkey.Load(keyDir)
	c.Assert(err, ErrorMatches, ".*unexpected bytes.*")
}

// TestKeySign makes sure the crypto.Signer works.
func (ks *keySuite) TestKeySign(c *C) {
	keyDir := filepath.Join(c.MkDir(), "identity")

	// Create a new identity key (first boot)
	signer, err := idkey.Generate(keyDir)
	c.Assert(err, IsNil)

	message := []byte("hello world")

	// Hash is optional for Ed25519, but crypto.Signer requires a hash function
	signature, err := signer.Sign(rand.Reader, message, crypto.Hash(0))
	c.Assert(err, IsNil)

	// Extract the public key
	pubKey, ok := signer.Public().(ed25519.PublicKey)
	c.Assert(ok, Equals, true)

	// Verify signature
	ok = ed25519.Verify(pubKey, message, signature)
	c.Assert(ok, Equals, true)
}

// BenchmarkKeyGeneration prints some performance metrics. To run this test
// use: go test -check.b
func (ks *keySuite) BenchmarkKeyGeneration(c *C) {
	for i := 0; i < c.N; i++ {
		keyDir := filepath.Join(c.MkDir(), "identity")
		_, err := idkey.Generate(keyDir)
		c.Assert(err, IsNil)
	}
}
