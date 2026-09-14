// Copyright (c) 2026 Canonical Ltd
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

package truststate_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/testutil"
)

// Hook up check.v1 into the "go test" runner.
func Test(t *testing.T) {
	testutil.PrintGoroutineLeaks(t, TestingT)
}

var serial int64

func nextSerial() *big.Int {
	serial++
	return big.NewInt(serial)
}

// generateCACert creates a self-signed CA certificate for use in tests, and
// returns its PEM encoding, the parsed certificate and its private key (so
// that leaf certificates can be generated and signed by it).
func generateCACert(c *C, commonName string) (pemBytes []byte, cert *x509.Certificate, key ed25519.PrivateKey) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	c.Assert(err, IsNil)

	tmpl := &x509.Certificate{
		SerialNumber:          nextSerial(),
		Subject:               pkix.Name{CommonName: commonName},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, pub, priv)
	c.Assert(err, IsNil)
	cert, err = x509.ParseCertificate(der)
	c.Assert(err, IsNil)

	pemBytes = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	return pemBytes, cert, priv
}

// generateLeafCert creates a leaf certificate signed by the given CA
// certificate and key, for use in verifying that a resolved trust context's
// CA pool actually trusts the expected certificate authority.
func generateLeafCert(c *C, ca *x509.Certificate, caKey ed25519.PrivateKey, commonName string) *x509.Certificate {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	c.Assert(err, IsNil)

	tmpl := &x509.Certificate{
		SerialNumber: nextSerial(),
		Subject:      pkix.Name{CommonName: commonName},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca, pub, caKey)
	c.Assert(err, IsNil)
	leaf, err := x509.ParseCertificate(der)
	c.Assert(err, IsNil)
	return leaf
}
