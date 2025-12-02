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

package client

import (
	"crypto/ed25519"
	"crypto/sha512"
	"crypto/tls"
	"crypto/x509"
	"encoding/base32"
	"errors"
	"fmt"
)

// verifyConnection verifies the incoming server TLS certificate.
func verifyConnection(state tls.ConnectionState, opts *Config) error {
	// We always expect two certificates from our server:
	//
	// state.PeerCertificates[0] - server TLS certificate
	// state.PeerCertificates[1] - server Identity certificate (root CA)
	certCount := len(state.PeerCertificates)
	if certCount != expectedServerCertCount {
		return fmt.Errorf("cannot find identity certificate: expected %d certificates, got %d", expectedServerCertCount, certCount)
	}
	// Make a local copy of the server identity certificate (root CA).
	serverIDCert := state.PeerCertificates[1]

	if opts.TLSServerFingerprint != "" {
		// Client supplied fingerprint must match the server identity.
		idFingerprint, err := getIdentityFingerprint(serverIDCert)
		if err != nil {
			return fmt.Errorf("cannot obtain identity fingerprint: %v", err)
		}
		if idFingerprint == opts.TLSServerFingerprint {
			// Fingerprint verification passed.
			return nil
		}
		return errors.New("server fingerprint mismatch")

	} else if opts.TLSServerIDCert != nil {
		// Verify the incoming server TLS certificate with the pinned
		// server identity certificate.
		roots := x509.NewCertPool()
		roots.AddCert(opts.TLSServerIDCert)
		verifyOpts := x509.VerifyOptions{
			Roots: roots,
		}
		incomingTLS := state.PeerCertificates[0]
		_, err := incomingTLS.Verify(verifyOpts)
		return err

	} else if opts.TLSServerInsecure {
		// Insecure server connection. Proceed with care.
		return nil
	}

	return errors.New("cannot verify server: see TLS config options")
}

// getIdentityFingerprint extracts the public key from the certificate and
// calculates the fingerprint.
func getIdentityFingerprint(cert *x509.Certificate) (string, error) {
	pubKey, ok := cert.PublicKey.(ed25519.PublicKey)
	if !ok {
		return "", errors.New("certificate must use Ed25519 public key")
	}
	hashBytes := sha512.Sum384(pubKey)
	fingerprint := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(hashBytes[:])
	return fingerprint, nil
}
