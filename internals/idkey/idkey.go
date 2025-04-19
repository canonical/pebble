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

// idkey supplies an identity key for a machine or device. This package
// provides an implementation based on an Ed25519 based key, currently
// only supporting a file based key storage solution. This can later be
// extended to support more secure hardware backed keystore, such as
// TPM, OP-TEE or UbiKey.
package idkey

import (
	"bytes"
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
	key    any
}

// New checks if an existing identity key exists, and loads the key if it
// does. It creates a new private identity key and persists it to disk if
// no key was found. Cases where explicit control is desired on when to
// generate or load can use GenerateKey and LoadKeyFromFile.
func New(keyDir string) (*IDKey, error) {
	keyPath := filepath.Join(keyDir, identityKeyFile)
	_, err := os.Stat(keyPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Generate a new key and persist.
			return GenerateKey(keyDir)
		}
		return nil, err
	}
	return LoadKeyFromFile(keyDir)
}

// GenerateKey generates a new identity key and persists it to disk. This
// function should only ever be called on the first boot of a machine or
// device, otherwise a new identity will be created.
//
// This function is equivalent to running:
//
// 	openssl genpkey -algorithm Ed25519 -out key.pem
func GenerateKey(keyDir string) (*IDKey, error) {
	key := &IDKey{
		keyDir: keyDir,
	}
	// Generate new ed25519 private key.
	err := key.newEd25519()
	if err != nil {
		return nil, err
	}
	// Persist to disk.
	err = key.save()
	if err != nil {
		return nil, err
	}
	return key, nil
}

// LoadKeyFromFile loads an existing identity key from disk.
func LoadKeyFromFile(keyDir string) (*IDKey, error) {
	key := &IDKey{
		keyDir: keyDir,
	}
	// Load from disk.
	err := key.load()
	if err != nil {
		return nil, err
	}
	return key, nil
}

// Public implements the crypto.Signer.
func (k *IDKey) Public() crypto.PublicKey {
	signer := k.key.(crypto.Signer)
	return signer.Public()
}

// Sign implements the crypto.Signer.
func (k *IDKey) Sign(rand io.Reader, digest []byte, opts crypto.SignerOpts) (signature []byte, err error) {
	signer := k.key.(crypto.Signer)
	return signer.Sign(rand, digest, opts)
}

// newEd25519 generates a new ed25519 key.
func (k *IDKey) newEd25519() error {
	var err error
	_, k.key, err = ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("cannot generate ed25519 key: %w", err)
	}
	return nil
}

// load loads the private identity key from storage.
func (k *IDKey) load() error {
	// Check the permissions are what we expect.
	_, err := os.Stat(k.keyDir)
	if err != nil {
		return fmt.Errorf("cannot find identity key: directory %q is not accessible", k.keyDir)
	}
	err = expectPermission(k.keyDir, 0o700)
	if err != nil {
		return fmt.Errorf("cannot verify identity key directory permissions: %w", err)
	}
	pemPath := filepath.Join(k.keyDir, identityKeyFile)
	err = expectPermission(pemPath, 0o600)
	if err != nil {
		return fmt.Errorf("cannot verify PEM permission for %q: %w", pemPath, err)
	}
	// Load the key.
	pemData, err := os.ReadFile(pemPath)
	if err != nil {
		return err
	}
	for {
		var block *pem.Block
		block, pemData = pem.Decode(pemData)
		if block == nil {
			break
		}
		switch block.Type {
		case "PRIVATE KEY":
			k.key, err = x509.ParsePKCS8PrivateKey(block.Bytes)
			if err != nil {
				return err
			}
		default:
			return fmt.Errorf("cannot load private identity key from block %q in PEM file %q", block.Type, pemPath)
		}
	}
	if k.key == nil {
		return fmt.Errorf("cannot find private identity key in PEM file %q", pemPath)
	}
	return nil
}

// save saves the private identity key to storage.
func (k *IDKey) save() error {
	// If the identity key directory does not yet exist, create it.
	_, err := os.Stat(k.keyDir)
	if os.IsNotExist(err) {
		// Create the leaf directory node with 0700 permissions.
		err = os.Mkdir(k.keyDir, 0o700)
	} else {
		err = expectPermission(k.keyDir, 0o700)
		if err != nil {
			return fmt.Errorf("cannot verify identity key directory permissions: %w", err)
		}
	}
	if err != nil {
		return fmt.Errorf("cannot create directory leaf of path %v: %w", k.keyDir, err)
	}
	// Create the new private identity file.
	pemPath := filepath.Join(k.keyDir, identityKeyFile)
	pemFile, err := os.OpenFile(pemPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, pemFile.Sync())
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
	var pemBuffer bytes.Buffer
	if err := pem.Encode(&pemBuffer, pemPrivateBlock); err != nil {
		return fmt.Errorf("cannot convert key to PEM: %w", err)
	}
	if _, err = pemFile.Write(pemBuffer.Bytes()); err != nil {
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
