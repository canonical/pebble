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

// idkey supplies an identity key for a machine, container or device. This
// package provides an implementation based on an Ed25519 based key, currently
// only supporting a file based key storage solution. This can later be
// extended to support more secure hardware backed keystores, such as TPM,
// OP-TEE or YubiKey.
package idkey

import (
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha512"
	"crypto/x509"
	"encoding/base32"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

const identityKeyFile = "key.pem"

// Identity key must implement a crypto signer.
var _ crypto.Signer = (*IDKey)(nil)

type IDKey struct {
	keyDir string
	key    ed25519.PrivateKey
}

// Get checks if an existing private identity key exists, and loads the key
// if it does. It creates a new private identity key and persists it to disk
// if no key was found. Cases where explicit control is desired on when to
// generate or load can use Generate and Load directly.
func Get(keyDir string) (*IDKey, error) {
	keyPath := filepath.Join(keyDir, identityKeyFile)
	exists, err := pathExists(keyPath)
	if err != nil {
		return nil, fmt.Errorf("cannot access key file %q: %w", keyPath, err)
	}
	if exists {
		return Load(keyDir)
	}
	return Generate(keyDir)
}

// Generate generates a new identity key and persists it to disk. This
// function should only ever be called on the first boot otherwise the
// existing identity will be overwritten.
//
// This function is equivalent to running:
//
//	openssl genpkey -algorithm Ed25519 -out key.pem
func Generate(keyDir string) (*IDKey, error) {
	k := &IDKey{
		keyDir: keyDir,
	}
	err := k.createDir()
	if err != nil {
		return nil, fmt.Errorf("cannot create identity directory: %w", err)
	}
	_, k.key, err = ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("cannot generate identity key: %w", err)
	}
	err = k.save()
	if err != nil {
		return nil, fmt.Errorf("cannot save identity key: %w", err)
	}
	return k, nil
}

// Load loads an existing identity key from disk.
func Load(keyDir string) (*IDKey, error) {
	key := &IDKey{
		keyDir: keyDir,
	}
	// Load from disk.
	err := key.load()
	if err != nil {
		return nil, fmt.Errorf("cannot load identity key: %w", err)
	}
	return key, nil
}

// createDir verifies the directory layout and permissions, and creates
// the leaf element of the key driectory if it does not yet exist.
func (k *IDKey) createDir() error {
	exists, err := pathExists(k.keyDir)
	if err != nil {
		return err
	}
	if exists {
		return expectPermission(k.keyDir, 0o700)
	}

	// Create the leaf directory node with 0o700 permissions.
	return os.Mkdir(k.keyDir, 0o700)
}

// Public implements part of the crypto.Signer interface.
func (k *IDKey) Public() crypto.PublicKey {
	return k.key.Public()
}

// Sign implements part of the crypto.Signer interface.
func (k *IDKey) Sign(rand io.Reader, digest []byte, opts crypto.SignerOpts) (signature []byte, err error) {
	return k.key.Sign(rand, digest, opts)
}

// load loads the private identity key from storage.
func (k *IDKey) load() error {
	exists, err := pathExists(k.keyDir)
	if err != nil {
		return fmt.Errorf("directory %q is not accessible: %w", k.keyDir, err)
	}
	if !exists {
		return fmt.Errorf("directory %q not found", k.keyDir)
	}
	err = expectPermission(k.keyDir, 0o700)
	if err != nil {
		return err
	}
	pemPath := filepath.Join(k.keyDir, identityKeyFile)
	err = expectPermission(pemPath, 0o600)
	if err != nil {
		return err
	}
	// Load the key.
	pemData, err := os.ReadFile(pemPath)
	if err != nil {
		return err
	}
	block, rest := pem.Decode(pemData)
	if block == nil || block.Type != "PRIVATE KEY" {
		return fmt.Errorf("missing 'PRIVATE KEY' block in %q", pemPath)
	}
	if len(rest) != 0 {
		return fmt.Errorf("unexpected bytes after 'PRIVATE KEY' block in %q", pemPath)
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return err
	}
	var ok bool
	k.key, ok = key.(ed25519.PrivateKey)
	if !ok {
		return fmt.Errorf("ed25519 type private key expected")
	}
	return nil
}

// save saves the private identity key to storage.
func (k *IDKey) save() (err error) {
	exists, err := pathExists(k.keyDir)
	if err != nil {
		return fmt.Errorf("directory %q is not accessible: %w", k.keyDir, err)
	}
	if exists {
		err = expectPermission(k.keyDir, 0o700)
		if err != nil {
			return err
		}
	} else {
		// Create the leaf directory node with 0700 permissions.
		err = os.Mkdir(k.keyDir, 0o700)
		if err != nil {
			return err
		}
	}

	// Create the new private identity file.
	pemPath := filepath.Join(k.keyDir, identityKeyFile)
	pemFile, err := os.OpenFile(pemPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		// err here refers to the named error return.
		err = errors.Join(err, pemFile.Close())
	}()
	keyBytes, err := x509.MarshalPKCS8PrivateKey(k.key)
	if err != nil {
		return err
	}
	pemPrivateBlock := &pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: keyBytes,
	}
	if err = pem.Encode(pemFile, pemPrivateBlock); err != nil {
		return err
	}
	if err = pemFile.Sync(); err != nil {
		return err
	}
	return nil
}

// Fingerprint returns the identity fingerprint. This is the SHA512/384 hash
// of the public key, encoded in base32 (without padding). This is a
// convenient shorthand form of the identity that can be used to identify
// a specfic machine or device.
func (k *IDKey) Fingerprint() string {
	publicBytes := k.Public().(ed25519.PublicKey)
	hashBytes := sha512.Sum384(publicBytes)
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(hashBytes[:])
}

// expectPermission return an error if the specified directory or file
// path is not matching the supplied permissions.
func expectPermission(path string, perm fs.FileMode) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	actualPerm := info.Mode().Perm()
	if actualPerm != perm {
		return fmt.Errorf("expected permission 0o%o (got 0o%o) for %q", perm, actualPerm, path)
	}
	return nil
}

// pathExists returns true of the path exists, false if it does not. If the
// operation reports an unrelated error, the error is returned.
func pathExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	return true, nil
}
