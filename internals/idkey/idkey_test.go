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
	firstBoot, err := idkey.GenerateKey(keyDir)
	c.Assert(err, IsNil)

	// Load the identity key (other boots)
	nextBoot, err := idkey.LoadKeyFromFile(keyDir)
	c.Assert(err, IsNil)

	// Both should be the same identity.
	c.Assert(firstBoot.Fingerprint(), Equals, nextBoot.Fingerprint())
}

// TestNew checks if the New() function correctly only creates a new
// identity the first time.
func (ks *keySuite) TestNew(c *C) {
	keyDir := filepath.Join(c.MkDir(), "identity")

	// Create a new identity key (first boot)
	firstBoot, err := idkey.New(keyDir)
	c.Assert(err, IsNil)

	// Load the identity key (other boots)
	nextBoot, err := idkey.New(keyDir)

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
	_, err = idkey.LoadKeyFromFile(keyDir)
	c.Assert(err, ErrorMatches, ".* expected permission 0o700 .*")

	// Saving
	_, err = idkey.GenerateKey(keyDir)
	c.Assert(err, ErrorMatches, ".* expected permission 0o700 .*")
}

// TestDirInvalid checks for a missing non-leaf directory. The caller
// must create this.
func (ks *keySuite) TestDirInvalid(c *C) {
	keyDir := filepath.Join(c.MkDir(), "foo/identity")

	// Saving
	_, err := idkey.GenerateKey(keyDir)
	c.Assert(err, ErrorMatches, "cannot create directory leaf .*")
}

// TestInvalidKey checks permission of the key file.
func (ks *keySuite) TestInvalidKey(c *C) {
	keyDir := filepath.Join(c.MkDir(), "identity")

	// Create a new identity key (first boot)
	_, err := idkey.GenerateKey(keyDir)
	c.Assert(err, IsNil)

	err = os.Chmod(filepath.Join(keyDir, "key.pem"), 0o644)
	c.Assert(err, IsNil)

	// Load the identity key (other boots)
	_, err = idkey.LoadKeyFromFile(keyDir)
	c.Assert(err, ErrorMatches, "cannot verify PEM permission .*")
}

// TestCorruptKey checks a corrupt key fails to load.
func (ks *keySuite) TestCorruptKey(c *C) {
	keyDir := filepath.Join(c.MkDir(), "identity")

	// Create a new identity key (first boot)
	_, err := idkey.GenerateKey(keyDir)
	c.Assert(err, IsNil)

	// Zero the existing file.
	f, err := os.OpenFile(filepath.Join(keyDir, "key.pem"), os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600)
	c.Assert(err, IsNil)
	err = f.Close()
	c.Assert(err, IsNil)

	// Load the identity key (other boots)
	_, err = idkey.LoadKeyFromFile(keyDir)
	c.Assert(err, ErrorMatches, "cannot find private identity key .*")
}

// MTestKeySign makes sure the crypto.Signer works.
func (ks *keySuite) TestKeySign(c *C) {
	keyDir := filepath.Join(c.MkDir(), "identity")

	// Create a new identity key (first boot)
	signer, err := idkey.GenerateKey(keyDir)
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
