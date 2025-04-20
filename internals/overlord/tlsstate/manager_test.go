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

package tlsstate_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"os"
	"path/filepath"
	"slices"
	"time"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/overlord/tlsstate"
)

// TestNoDirectory checks if the manager can successfully create a new
// identity certificate and TLS keypair if the leaf directory does not
// yet exist.
func (ts *tlsSuite) TestNoDirectory(c *C) {
	_, key, err := ed25519.GenerateKey(rand.Reader)
	c.Assert(err, IsNil)

	tlsDir := filepath.Join(c.MkDir(), "tls")
	mgr, err := tlsstate.NewManager(tlsDir, key)
	c.Assert(err, IsNil)

	_, err = mgr.GetCertificate(nil)
	c.Assert(err, IsNil)
}

// TestDirectoryInvalidPerm checks if startup will fail if the TLS directory
// does not have the correct permissions.
func (ts *tlsSuite) TestDirectoryInvalidPerm(c *C) {
	_, key, err := ed25519.GenerateKey(rand.Reader)
	c.Assert(err, IsNil)

	tlsDir := filepath.Join(c.MkDir(), "tls")
	err = os.MkdirAll(tlsDir, 0740)
	c.Assert(err, IsNil)

	_, err = tlsstate.NewManager(tlsDir, key)
	c.Assert(err, ErrorMatches, ".* expected permission 0o700 .*")
}

// TestKeypairDirNoParent checks if the manager will fail to create the
// parent directory it does not own.
func (ts *tlsSuite) TestKeypairDirNoParent(c *C) {
	_, key, err := ed25519.GenerateKey(rand.Reader)
	c.Assert(err, IsNil)

	tlsDir := filepath.Join(c.MkDir(), "something/tls")

	_, err = tlsstate.NewManager(tlsDir, key)
	c.Assert(err, ErrorMatches, "cannot create TLS directory.*")
}

// TestInvalidIDCertContent checks if we detect an invalid PEM file for
// the identity certificate.
func (ts *tlsSuite) TestInvalidIDCertContent(c *C) {
	_, key, err := ed25519.GenerateKey(rand.Reader)
	c.Assert(err, IsNil)

	tlsDir := filepath.Join(c.MkDir(), "tls")

	mgr, err := tlsstate.NewManager(tlsDir, key)
	c.Assert(err, IsNil)

	f, err := os.OpenFile(filepath.Join(tlsDir, "identity.pem"), os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600)
	c.Assert(err, IsNil)
	err = f.Close()
	c.Assert(err, IsNil)

	_, err = mgr.GetCertificate(nil)
	c.Assert(err, ErrorMatches, ".*empty PEM file.*")
}

// TestInvalidIDCertPerm checks if we detect an invalid permission on
// the identity certificate.
func (ts *tlsSuite) TestInvalidIDCertPerm(c *C) {
	_, key, err := ed25519.GenerateKey(rand.Reader)
	c.Assert(err, IsNil)

	tlsDir := filepath.Join(c.MkDir(), "tls")
	mgr, err := tlsstate.NewManager(tlsDir, key)
	c.Assert(err, IsNil)

	// Create the identity certificate on demand.
	_, err = mgr.GetCertificate(nil)
	c.Assert(err, IsNil)

	err = os.Chmod(filepath.Join(tlsDir, "identity.pem"), 0644)
	c.Assert(err, IsNil)

	// Simulate a process restart by creating a new manager.
	mgr, err = tlsstate.NewManager(tlsDir, key)
	c.Assert(err, IsNil)

	// Create the identity certificate on demand.
	_, err = mgr.GetCertificate(nil)
	c.Assert(err, ErrorMatches, ".*expected permission.*")
}

// TestTLSServerClient checks if the identity CA cert works while we are
// rotating TLS keypairs.
func (ts *tlsSuite) TestTLSServerClient(c *C) {
	systemTime := "2000-01-01"

	restoreTLSCertValidity := tlsstate.FakeTLSCertValidity(time.Hour)
	defer restoreTLSCertValidity()

	tlsDir := filepath.Join(c.MkDir(), "tls")

	_, key, err := ed25519.GenerateKey(rand.Reader)
	c.Assert(err, IsNil)
	mgr, err := tlsstate.NewManager(tlsDir, key)
	c.Assert(err, IsNil)

	// Start the HTTPS server.
	shutdown := ts.testTLSServer(c, mgr.GetCertificate)
	defer shutdown()

	// Simulate a client pairing procedure, after which we trust the
	// identity certificate. This initial session will also ensure
	// the identity and TLS certs are in place.
	restore, clock := tlsstate.FakeSystemTime(systemTime, 0)
	certs, err := ts.testTLSInsecureClient(c, clock)
	c.Assert(err, IsNil)
	tlsCert := certs[0]
	ca := certs[1]
	restore()

	// Ensure the client identity CA works even if the TLS cert rotates.
	previousTLSCerts := []*x509.Certificate{tlsCert}
	for i := 1; i <= 10; i++ {
		restore, clock := tlsstate.FakeSystemTime(systemTime, time.Duration(i)*24*time.Hour)

		// Test a trusted client connection (we use the identity CA cert).
		certs, err = ts.testTLSVerifiedClient(c, ca, clock)
		c.Assert(err, IsNil)

		tlsCert := certs[0]

		// Ensure the TLS certificate was not seen before because it expires every hour.
		c.Assert(slices.Contains(previousTLSCerts, tlsCert), Equals, false)

		previousTLSCerts = append(previousTLSCerts, tlsCert)

		restore()
	}
}

// TestTLSServerClientTLSReuse checks TLS certificates are not rotated while
// they are valid.
func (ts *tlsSuite) TestTLSServerClientTLSReuse(c *C) {
	systemTime := "2000-01-01"

	restoreTLSCertValidity := tlsstate.FakeTLSCertValidity(24 * time.Hour)
	defer restoreTLSCertValidity()

	tlsDir := filepath.Join(c.MkDir(), "tls")

	_, key, err := ed25519.GenerateKey(rand.Reader)
	c.Assert(err, IsNil)
	mgr, err := tlsstate.NewManager(tlsDir, key)
	c.Assert(err, IsNil)

	// Start the HTTPS server.
	shutdown := ts.testTLSServer(c, mgr.GetCertificate)
	defer shutdown()

	// Simulate a client pairing procedure, after which we trust the
	// identity certificate. This initial session will also ensure
	// the identity and TLS certs are in place.
	restore, clock := tlsstate.FakeSystemTime(systemTime, 0)
	certs, err := ts.testTLSInsecureClient(c, clock)
	c.Assert(err, IsNil)
	tlsCert := certs[0]
	ca := certs[1]
	restore()

	// Ensure the client identity CA works even if the TLS cert rotates.
	for i := 1; i <= 10; i++ {
		// Time moved forwards 1 hour at a time.
		restore, clock := tlsstate.FakeSystemTime(systemTime, time.Duration(i)*time.Hour)

		// Test a trusted client connection (we use the identity CA cert).
		certs, err = ts.testTLSVerifiedClient(c, ca, clock)
		c.Assert(err, IsNil)

		// Should stay the same
		c.Assert(tlsCert.Equal(certs[0]), Equals, true)

		restore()
	}
}

// TestTLSServerClientRenewWindow checks that a TLS certificate is rotated
// as soon as the Renewal Window is entered (to avoid a race with the expiry
// time during the TLS handshake).
func (ts *tlsSuite) TestTLSServerClientRenewWindow(c *C) {
	systemTime := "2000-01-01"

	restoreTLSCertValidity := tlsstate.FakeTLSCertValidity(time.Hour)
	defer restoreTLSCertValidity()

	restoreTLSCertRenewWindow := tlsstate.FakeTLSCertRenewWindow(60 * time.Second)
	defer restoreTLSCertRenewWindow()

	tlsDir := filepath.Join(c.MkDir(), "tls")

	_, key, err := ed25519.GenerateKey(rand.Reader)
	c.Assert(err, IsNil)
	mgr, err := tlsstate.NewManager(tlsDir, key)
	c.Assert(err, IsNil)

	// Start the HTTPS server.
	shutdown := ts.testTLSServer(c, mgr.GetCertificate)
	defer shutdown()

	// Simulate a client pairing procedure, after which we trust the
	// identity certificate. This initial session will also ensure
	// the identity and TLS certs are in place.
	restore, clock := tlsstate.FakeSystemTime(systemTime, 0)
	certs, err := ts.testTLSInsecureClient(c, clock)
	c.Assert(err, IsNil)
	tlsCert1 := certs[0]
	ca := certs[1]
	restore()

	// Set the system time 5 to seconds before the actual timeout, but
	// within the renewal window.
	renewWindow := 5 * time.Second
	restore, clock = tlsstate.FakeSystemTime(systemTime, time.Hour-renewWindow)
	defer restore()

	// Test a trusted client connection (we use the identity CA cert).
	certs, err = ts.testTLSVerifiedClient(c, ca, clock)
	c.Assert(err, IsNil)

	tlsCert2 := certs[0]
	// Validity duration should still be 1 hour
	c.Assert(tlsCert1.NotAfter.Sub(tlsCert1.NotBefore), Equals, time.Hour)
	c.Assert(tlsCert2.NotAfter.Sub(tlsCert2.NotBefore), Equals, time.Hour)
	// Second certificate should have been rotated 'renewWindow' seconds early
	c.Assert(tlsCert1.NotAfter.Sub(tlsCert2.NotBefore), Equals, renewWindow)
}

// TestTLSServerClientIDRotate checks that when the ID certificate rotates, the
// TLS certificate will also rotate, and the client will stop working.
func (ts *tlsSuite) TestTLSServerClientIDRotate(c *C) {
	systemTime := "2000-01-01"

	restoreTLSCertValidity := tlsstate.FakeTLSCertValidity(24 * time.Hour)
	defer restoreTLSCertValidity()

	restoreIDCertValidity := tlsstate.FakeIDCertValidity(12 * time.Hour)
	defer restoreIDCertValidity()

	tlsDir := filepath.Join(c.MkDir(), "tls")

	_, key, err := ed25519.GenerateKey(rand.Reader)
	c.Assert(err, IsNil)
	mgr, err := tlsstate.NewManager(tlsDir, key)
	c.Assert(err, IsNil)

	// Start the HTTPS server.
	shutdown := ts.testTLSServer(c, mgr.GetCertificate)
	defer shutdown()

	// Simulate a client pairing procedure, after which we trust the
	// identity certificate. This initial session will also ensure
	// the identity and TLS certs are in place.
	restore, clock := tlsstate.FakeSystemTime(systemTime, 0)
	certs, err := ts.testTLSInsecureClient(c, clock)
	c.Assert(err, IsNil)
	tlsCert := certs[0]
	ca := certs[1]
	restore()

	// 1 hour forward
	restore, clock = tlsstate.FakeSystemTime(systemTime, time.Hour)
	// Test a trusted client connection (we use the identity CA cert).
	certs, err = ts.testTLSVerifiedClient(c, ca, clock)
	c.Assert(err, IsNil)
	// TLS certificate stays the same
	c.Assert(tlsCert.Equal(certs[0]), Equals, true)
	restore()

	// 14 hours forward (ID cert expires)
	restore, clock = tlsstate.FakeSystemTime(systemTime, 14*time.Hour)
	// Test a trusted client connection (we use the identity CA cert).
	_, err = ts.testTLSVerifiedClient(c, ca, clock)
	c.Assert(err, ErrorMatches, ".*Root CA verify failed.*")
	// Test non-verified connection.
	certs, err = ts.testTLSInsecureClient(c, clock)
	c.Assert(err, IsNil)
	// ID certificate changed
	c.Assert(ca.Equal(certs[1]), Equals, false)
	// TLS certificate changed.
	c.Assert(tlsCert.Equal(certs[0]), Equals, false)
	restore()
}

// TestTLSServerClientIDKeyChange checks that if the crypto.Signer key changes,
// the ID certificate and the TLS certificate will rotate, and the client
// will stop working.
func (ts *tlsSuite) TestTLSServerClientIDKeyChange(c *C) {
	systemTime := "2000-01-01"

	restoreTLSCertValidity := tlsstate.FakeTLSCertValidity(2 * time.Hour)
	defer restoreTLSCertValidity()

	restoreIDCertValidity := tlsstate.FakeIDCertValidity(24 * time.Hour)
	defer restoreIDCertValidity()

	tlsDir := filepath.Join(c.MkDir(), "tls")

	_, key, err := ed25519.GenerateKey(rand.Reader)
	c.Assert(err, IsNil)
	mgr, err := tlsstate.NewManager(tlsDir, key)
	c.Assert(err, IsNil)

	// Start the HTTPS server.
	shutdown := ts.testTLSServer(c, mgr.GetCertificate)
	// Simulate a client pairing procedure, after which we trust the
	// identity certificate. This initial session will also ensure
	// the identity and TLS certs are in place.
	restore, clock := tlsstate.FakeSystemTime(systemTime, 0)
	certs, err := ts.testTLSInsecureClient(c, clock)
	c.Assert(err, IsNil)
	tlsCert := certs[0]
	ca := certs[1]
	restore()
	// Shut down the HTTPS server.
	shutdown()

	// 1 hours forward.
	restore, clock = tlsstate.FakeSystemTime(systemTime, time.Hour)
	defer restore()

	// This simulates a process restart, after which we should detect
	// the crypto.Signer no longer gives us the same private key.
	_, key, err = ed25519.GenerateKey(rand.Reader)
	c.Assert(err, IsNil)
	mgr, err = tlsstate.NewManager(tlsDir, key)
	c.Assert(err, IsNil)

	// Start the HTTPS server.
	shutdown = ts.testTLSServer(c, mgr.GetCertificate)
	// Test a trusted client connection (we use the identity CA cert).
	_, err = ts.testTLSVerifiedClient(c, ca, clock)
	c.Assert(err, ErrorMatches, ".*Root CA verify failed.*")
	// Test non-verified connection.
	certs, err = ts.testTLSInsecureClient(c, clock)
	c.Assert(err, IsNil)
	// ID certificate changed
	c.Assert(ca.Equal(certs[1]), Equals, false)
	// TLS certificate changed.
	c.Assert(tlsCert.Equal(certs[0]), Equals, false)
	// Shut down the HTTPS server.
	shutdown()
}
