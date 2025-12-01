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
	"crypto/x509"
	"crypto/x509/pkix"
	"os"
	"path/filepath"
	"slices"
	"time"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/cmd"
	"github.com/canonical/pebble/internals/overlord/tlsstate"
)

// TestNoDirectory checks if the manager can successfully create a new
// identity certificate and TLS keypair if the leaf directory does not
// yet exist.
func (ts *tlsSuite) TestNoDirectory(c *C) {
	tlsDir := filepath.Join(c.MkDir(), "tls")

	key := newIDKey(c)
	mgr := tlsstate.NewManager(tlsDir, key)
	_, err := mgr.GetCertificate(nil)
	c.Assert(err, IsNil)
}

// TestDirectoryInvalidPerm checks if startup will fail if the TLS directory
// does not have the correct permissions.
func (ts *tlsSuite) TestDirectoryInvalidPerm(c *C) {
	tlsDir := filepath.Join(c.MkDir(), "tls")
	err := os.MkdirAll(tlsDir, 0o740)
	c.Assert(err, IsNil)

	key := newIDKey(c)
	mgr := tlsstate.NewManager(tlsDir, key)
	_, err = mgr.GetCertificate(nil)
	c.Assert(err, ErrorMatches, ".* expected permission 0o700 .*")
}

// TestKeypairDirNoParent checks if the manager will fail to create the
// parent directory it does not own.
func (ts *tlsSuite) TestKeypairDirNoParent(c *C) {
	tlsDir := filepath.Join(c.MkDir(), "something/tls")

	key := newIDKey(c)
	mgr := tlsstate.NewManager(tlsDir, key)
	_, err := mgr.GetCertificate(nil)
	c.Assert(err, ErrorMatches, "cannot create TLS directory.*")
}

// TestInvalidIDCertContent checks if we detect an invalid PEM file for
// the identity certificate.
func (ts *tlsSuite) TestInvalidIDCertContent(c *C) {
	tlsDir := filepath.Join(c.MkDir(), "tls")
	err := os.MkdirAll(tlsDir, 0o700)
	c.Assert(err, IsNil)

	key := newIDKey(c)
	mgr := tlsstate.NewManager(tlsDir, key)

	// Empty the file.
	f, err := os.OpenFile(filepath.Join(tlsDir, "identity.pem"), os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600)
	c.Assert(err, IsNil)
	err = f.Close()
	c.Assert(err, IsNil)

	_, err = mgr.GetCertificate(nil)
	c.Assert(err, ErrorMatches, ".*missing 'CERTIFICATE' block.*")
}

// TestIDCertExtraBytes checks if we detect unexpected bytes following
// the identity certificate.
func (ts *tlsSuite) TestIDCertExtraBytes(c *C) {
	tlsDir := filepath.Join(c.MkDir(), "tls")
	err := os.MkdirAll(tlsDir, 0o700)
	c.Assert(err, IsNil)

	key := newIDKey(c)
	mgr := tlsstate.NewManager(tlsDir, key)

	// Generate certificates on demand.
	_, err = mgr.GetCertificate(nil)
	c.Assert(err, IsNil)

	// Append some random bytes at the end.
	f, err := os.OpenFile(filepath.Join(tlsDir, "identity.pem"), os.O_RDWR|os.O_APPEND, 0o600)
	c.Assert(err, IsNil)
	_, err = f.Write([]byte("\n1234567890"))
	c.Assert(err, IsNil)
	err = f.Close()
	c.Assert(err, IsNil)

	// Simulate a process restart by creating a new manager.
	mgr = tlsstate.NewManager(tlsDir, key)
	_, err = mgr.GetCertificate(nil)
	c.Assert(err, ErrorMatches, ".*unexpected bytes.*")
}

// TestInvalidIDCertPerm checks if we detect an invalid permission on
// the identity certificate.
func (ts *tlsSuite) TestInvalidIDCertPerm(c *C) {
	tlsDir := filepath.Join(c.MkDir(), "tls")

	key := newIDKey(c)
	mgr := tlsstate.NewManager(tlsDir, key)

	// Generate certificates on demand.
	_, err := mgr.GetCertificate(nil)
	c.Assert(err, IsNil)

	// Make the permission invalid.
	err = os.Chmod(filepath.Join(tlsDir, "identity.pem"), 0o644)
	c.Assert(err, IsNil)

	// Simulate a process restart by creating a new manager.
	mgr = tlsstate.NewManager(tlsDir, key)
	_, err = mgr.GetCertificate(nil)
	c.Assert(err, ErrorMatches, ".*expected permission.*")
}

// TestTLSServerClient checks if the identity certificate works as the root CA
// while we are rotating TLS keypairs.
func (ts *tlsSuite) TestTLSServerClient(c *C) {
	restoreTLSCertValidity := tlsstate.FakeTLSCertValidity(time.Hour)
	defer restoreTLSCertValidity()

	tlsDir := filepath.Join(c.MkDir(), "tls")
	key := newIDKey(c)
	mgr := tlsstate.NewManager(tlsDir, key)

	// Start the HTTPS server.
	shutdownHTTPSServer := ts.testTLSServer(c, mgr.GetCertificate)
	defer shutdownHTTPSServer()

	testBaseTime := getTestTime(2000, 1, 1)
	restoreTime := tlsstate.FakeTimeNow(testBaseTime)
	// Use an insecure first connection to obtain the certificates. This will
	// happen during a trust exchange procedure, after which we pin (trust) the
	// identity certificate by our client.
	certs, err := ts.testTLSInsecureClient(c, testBaseTime)
	c.Assert(err, IsNil)
	tlsCert := certs[0]
	idCert := certs[1]
	restoreTime()

	previousTLSCerts := []*x509.Certificate{tlsCert}
	for i := 1; i <= 10; i++ {
		// Move the time forward by 1 hour on each iteration.
		testTime := testBaseTime.Add(time.Duration(i) * 24 * time.Hour)
		restoreTime := tlsstate.FakeTimeNow(testTime)

		// Test a trusted client connection (we use the identity as the root CA).
		certs, err = ts.testTLSVerifiedClient(c, idCert, testTime)
		c.Assert(err, IsNil)

		tlsCert := certs[0]

		// Ensure the TLS certificate was not seen before. We expect a new
		// certificate to get generated every hour.
		c.Assert(slices.Contains(previousTLSCerts, tlsCert), Equals, false)
		previousTLSCerts = append(previousTLSCerts, tlsCert)
		restoreTime()
	}
}

// TestTLSServerClientTLSReuse checks TLS certificates are not rotated while
// they are valid.
func (ts *tlsSuite) TestTLSServerClientTLSReuse(c *C) {
	restoreTLSCertValidity := tlsstate.FakeTLSCertValidity(24 * time.Hour)
	defer restoreTLSCertValidity()

	tlsDir := filepath.Join(c.MkDir(), "tls")
	key := newIDKey(c)
	mgr := tlsstate.NewManager(tlsDir, key)

	// Start the HTTPS server.
	shutdownHTTPSServer := ts.testTLSServer(c, mgr.GetCertificate)
	defer shutdownHTTPSServer()

	testBaseTime := getTestTime(2000, 1, 1)
	restoreTime := tlsstate.FakeTimeNow(testBaseTime)
	// Use an insecure first connection to obtain the certificates. This will
	// happen during a trust exchange procedure, after which we pin (trust) the
	// identity certificate by our client.
	certs, err := ts.testTLSInsecureClient(c, testBaseTime)
	c.Assert(err, IsNil)
	tlsCert := certs[0]
	idCert := certs[1]
	restoreTime()

	for i := 1; i <= 10; i++ {
		// Move the time forward by 1 hour on each iteration.
		testTime := testBaseTime.Add(time.Duration(i) * time.Hour)
		restoreTime := tlsstate.FakeTimeNow(testTime)

		// Test a trusted client connection (we use the identity as the root CA).
		certs, err = ts.testTLSVerifiedClient(c, idCert, testTime)
		c.Assert(err, IsNil)

		// Should stay the same
		c.Assert(tlsCert.Equal(certs[0]), Equals, true)
		restoreTime()
	}
}

// TestTLSServerClientRenewWindow checks that a TLS certificate is rotated
// as soon as the Renewal Window is entered (to avoid a race with the expiry
// time during the TLS handshake).
func (ts *tlsSuite) TestTLSServerClientRenewWindow(c *C) {
	restoreTLSCertValidity := tlsstate.FakeTLSCertValidity(time.Hour)
	defer restoreTLSCertValidity()

	restoreTLSCertRenewWindow := tlsstate.FakeTLSCertRenewWindow(60 * time.Second)
	defer restoreTLSCertRenewWindow()

	key := newIDKey(c)
	tlsDir := filepath.Join(c.MkDir(), "tls")
	mgr := tlsstate.NewManager(tlsDir, key)

	// Start the HTTPS server.
	shutdownHTTPSServer := ts.testTLSServer(c, mgr.GetCertificate)
	defer shutdownHTTPSServer()

	testBaseTime := getTestTime(2000, 1, 1)
	restoreTime := tlsstate.FakeTimeNow(testBaseTime)
	// Use an insecure first connection to obtain the certificates. This will
	// happen during a trust exchange procedure, after which we pin (trust) the
	// identity certificate by our client.
	certs, err := ts.testTLSInsecureClient(c, testBaseTime)
	c.Assert(err, IsNil)
	tlsCert1 := certs[0]
	idCert := certs[1]
	restoreTime()

	// The normal rotation period is 1 hour. We will now move the time on
	// to 59 seconds before the end of the 1 hour. This falls within the
	// configured 60 second renewal window just before expiry. This now
	// means that if a new TLS session is requested, a new certificate
	// should be returned, instead of using the unexpired current one.
	renewalPoint := (59 * time.Second)
	testTime := testBaseTime.Add(time.Hour - renewalPoint)
	restoreTime = tlsstate.FakeTimeNow(testTime)
	defer restoreTime()

	// Test a trusted client connection (we use the identity as the root CA).
	certs, err = ts.testTLSVerifiedClient(c, idCert, testTime)
	c.Assert(err, IsNil)

	tlsCert2 := certs[0]
	// Validity duration should still be 1 hour
	c.Assert(tlsCert1.NotAfter.Sub(tlsCert1.NotBefore), Equals, time.Hour)
	c.Assert(tlsCert2.NotAfter.Sub(tlsCert2.NotBefore), Equals, time.Hour)
	// Second certificate should have been rotated 'renewWindow' seconds early
	c.Assert(tlsCert1.NotAfter.Sub(tlsCert2.NotBefore), Equals, renewalPoint)
}

// TestTLSServerClientIDExpires checks that when the ID certificate expires, the
// TLS certificate will not rotate, and the client certificate verification
// will fail.
func (ts *tlsSuite) TestTLSServerClientIDExpires(c *C) {
	restoreTLSCertValidity := tlsstate.FakeTLSCertValidity(24 * time.Hour)
	defer restoreTLSCertValidity()

	restoreIDCertValidity := tlsstate.FakeIDCertValidity(12 * time.Hour)
	defer restoreIDCertValidity()

	key := newIDKey(c)
	tlsDir := filepath.Join(c.MkDir(), "tls")
	mgr := tlsstate.NewManager(tlsDir, key)

	// Start the HTTPS server.
	shutdownHTTPSServer := ts.testTLSServer(c, mgr.GetCertificate)
	defer shutdownHTTPSServer()

	testBaseTime := getTestTime(2000, 1, 1)
	restoreTime := tlsstate.FakeTimeNow(testBaseTime)
	// Use an insecure first connection to obtain the certificates. This will
	// happen during a trust exchange procedure, after which we pin (trust) the
	// identity certificate by our client.
	certs, err := ts.testTLSInsecureClient(c, testBaseTime)
	c.Assert(err, IsNil)
	tlsCert := certs[0]
	idCert := certs[1]
	restoreTime()

	// Move the time forward by 1 hour.
	testTime := testBaseTime.Add(time.Hour)
	restoreTime = tlsstate.FakeTimeNow(testTime)
	// Test a trusted client connection (we use the identity as the root CA).
	certs, err = ts.testTLSVerifiedClient(c, idCert, testTime)
	c.Assert(err, IsNil)
	// TLS certificate stays the same
	c.Assert(tlsCert.Equal(certs[0]), Equals, true)
	restoreTime()

	// Move the time forward by 14 hours (ID cert expires).
	testTime = testBaseTime.Add(14 * time.Hour)
	restoreTime = tlsstate.FakeTimeNow(testTime)
	// Test a trusted client connection (we use the identity as the root CA).
	_, err = ts.testTLSVerifiedClient(c, idCert, testTime)
	c.Assert(err, ErrorMatches, ".*Root CA verify failed.*")
	// ID certificate did not change (and is expired)
	c.Assert(idCert.Equal(certs[1]), Equals, true)
	c.Assert(certs[1].NotAfter.Before(testTime), Equals, true)
	// TLS certificate did not change.
	c.Assert(tlsCert.Equal(certs[0]), Equals, true)
	// Test non-verified connection (which should still work).
	certs, err = ts.testTLSInsecureClient(c, testTime)
	c.Assert(err, IsNil)
	// ID certificate did not change (and is expired)
	c.Assert(idCert.Equal(certs[1]), Equals, true)
	c.Assert(certs[1].NotAfter.Before(testTime), Equals, true)
	// TLS certificate did not change.
	c.Assert(tlsCert.Equal(certs[0]), Equals, true)
	restoreTime()
}

// TestTLSServerClientIDKeyChange checks that if the crypto.Signer key changes,
// the ID certificate and the TLS certificate will rotate, and the client
// verification using the pinned identity certificate will fail.
func (ts *tlsSuite) TestTLSServerClientIDKeyChange(c *C) {
	restoreTLSCertValidity := tlsstate.FakeTLSCertValidity(2 * time.Hour)
	defer restoreTLSCertValidity()

	restoreIDCertValidity := tlsstate.FakeIDCertValidity(24 * time.Hour)
	defer restoreIDCertValidity()

	key := newIDKey(c)
	tlsDir := filepath.Join(c.MkDir(), "tls")
	mgr := tlsstate.NewManager(tlsDir, key)

	// Start the HTTPS server.
	shutdownHTTPSServer := ts.testTLSServer(c, mgr.GetCertificate)

	testBaseTime := getTestTime(2000, 1, 1)
	restoreTime := tlsstate.FakeTimeNow(testBaseTime)
	// Use an insecure first connection to obtain the certificates. This will
	// happen during a trust exchange procedure, after which we pin (trust) the
	// identity certificate by our client.
	certs, err := ts.testTLSInsecureClient(c, testBaseTime)
	c.Assert(err, IsNil)
	tlsCert := certs[0]
	idCert := certs[1]
	restoreTime()

	// Shut down the HTTPS server.
	shutdownHTTPSServer()

	// 1 hours forward.
	testTime := testBaseTime.Add(time.Hour)
	restoreTime = tlsstate.FakeTimeNow(testTime)
	defer restoreTime()

	// This simulates a process restart, after which we should detect
	// the crypto.Signer no longer gives us the same private key.
	key = newIDKey(c)
	mgr = tlsstate.NewManager(tlsDir, key)

	// Start the HTTPS server.
	shutdownHTTPSServer = ts.testTLSServer(c, mgr.GetCertificate)

	// Test a trusted client connection (we use the identity as the root CA).
	_, err = ts.testTLSVerifiedClient(c, idCert, testTime)
	c.Assert(err, ErrorMatches, ".*Root CA verify failed.*")
	// Test non-verified connection.
	certs, err = ts.testTLSInsecureClient(c, testTime)
	c.Assert(err, IsNil)
	// ID certificate changed
	c.Assert(idCert.Equal(certs[1]), Equals, false)
	// TLS certificate changed.
	c.Assert(tlsCert.Equal(certs[0]), Equals, false)

	// Shut down the HTTPS server.
	shutdownHTTPSServer()
}

// BenchmarkIDTLSCertGen prints some performance metrics related to the worse case
// startup condition where both the identity certificate and TLS keypair must be
// generated. To run this test use: go test -check.b
func (ts *tlsSuite) BenchmarkIDTLSCertGen(c *C) {
	key := newIDKey(c)

	for i := 0; i < c.N; i++ {
		// New unique temporary directory (so identity cert must be re-created).
		tlsDir := filepath.Join(c.MkDir(), "tls")

		mgr := tlsstate.NewManager(tlsDir, key)

		// Create identity and TLS certificates on demand.
		_, err := mgr.GetCertificate(nil)
		c.Assert(err, IsNil)
	}
}

// TestDefaultCertSubject tests that the name is no longer than 64 bytes.
func (ts *tlsSuite) TestDefaultCertSubject(c *C) {
	tests := []struct {
		fingerprint     string
		programName     string
		expectedByteLen int
	}{{
		fingerprint:     "",
		programName:     "",
		expectedByteLen: 1,
	}, {
		fingerprint: "",
		// 70 chars
		programName: "AAAAAAAAAABBBBBBBBBBAAAAAAAAAABBBBBBBBBBAAAAAAAAAABBBBBBBBBBAAAAAAAAAABBBBBBBBBB",
		// program name is max 55 bytes, plus the separator.
		expectedByteLen: 56,
	}, {
		fingerprint: "",
		// 10 chars
		programName:     "AAAAAAAAAA",
		expectedByteLen: 11,
	}, {
		// 5 chars
		fingerprint: "CCCCC",
		// 70 chars
		programName: "AAAAAAAAAABBBBBBBBBBAAAAAAAAAABBBBBBBBBBAAAAAAAAAABBBBBBBBBBAAAAAAAAAABBBBBBBBBB",
		// program name is max 55 bytes, plus the separator and 5 bytes from the fingerprint.
		expectedByteLen: 61,
	}, {
		// 10 chars
		fingerprint: "CCCCCCCCCC",
		// 70 chars
		programName: "AAAAAAAAAABBBBBBBBBBAAAAAAAAAABBBBBBBBBBAAAAAAAAAABBBBBBBBBBAAAAAAAAAABBBBBBBBBB",
		// program name is max 55 bytes, plus the separator and max 8 chars from the fingerprint.
		expectedByteLen: 64,
	}, {
		// 10 chars
		fingerprint: "CCCCCCCCCC",
		programName: "",
		// fingerprint is max 8 bytes, plus the separator.
		expectedByteLen: 9,
	}}
	for _, t := range tests {
		cmd.ProgramName = t.programName
		s := tlsstate.DefaultCertSubject(t.fingerprint)
		c.Assert(s.CommonName, HasLen, t.expectedByteLen)
	}
}

// TestTLSServerClientCustomTemplates checks that we can provide custom
// X509 certificate templates for the identity and tls certificates.
func (ts *tlsSuite) TestTLSServerClientCustomTemplates(c *C) {
	restoreTLSCertValidity := tlsstate.FakeTLSCertValidity(time.Hour)
	defer restoreTLSCertValidity()

	key := newIDKey(c)
	tlsDir := filepath.Join(c.MkDir(), "tls")
	mgr := tlsstate.NewManager(tlsDir, key)

	// For the identity certificate.
	idTemplate := &x509.Certificate{
		Subject: pkix.Name{
			Country:            []string{"ZA"},
			Organization:       []string{"org"},
			OrganizationalUnit: []string{"unit"},
			Locality:           []string{"foo"},
			Province:           []string{"wc"},
			StreetAddress:      []string{"foo drive"},
			PostalCode:         []string{"34f55s"},
			SerialNumber:       "12345",
			CommonName:         "commonid",
		},
		DNSNames: []string{
			"id.local",
			"id2.local",
		},
		EmailAddresses: []string{
			"id@example.com",
			"id2@example.com",
		},
	}
	// For the tls certificate.
	tlsTemplate := &x509.Certificate{
		Subject: pkix.Name{
			Country:            []string{"ZA"},
			Organization:       []string{"org"},
			OrganizationalUnit: []string{"unit"},
			Locality:           []string{"bar"},
			Province:           []string{"wc"},
			StreetAddress:      []string{"bar drive"},
			PostalCode:         []string{"34f55s"},
			SerialNumber:       "67890",
			CommonName:         "commontls",
		},
		DNSNames: []string{
			"tls.local",
			"tls2.local",
		},
		EmailAddresses: []string{
			"tls@example.com",
			"tls2@example.com",
		},
	}
	mgr.SetX509Templates(idTemplate, tlsTemplate)

	// Start the HTTPS server.
	shutdownHTTPSServer := ts.testTLSServer(c, mgr.GetCertificate)
	defer shutdownHTTPSServer()

	testBaseTime := getTestTime(2000, 1, 1)
	restoreTime := tlsstate.FakeTimeNow(testBaseTime)
	certs, err := ts.testTLSInsecureClient(c, testBaseTime)
	c.Assert(err, IsNil)
	tlsCert := certs[0]
	idCert := certs[1]
	restoreTime()

	// Check the TLS certificate.
	c.Assert(tlsCert.Subject.String(), Equals, tlsTemplate.Subject.String())
	if !slices.Equal(tlsCert.DNSNames, tlsTemplate.DNSNames) {
		c.Fail()
	}
	if !slices.Equal(tlsCert.EmailAddresses, tlsTemplate.EmailAddresses) {
		c.Fail()
	}
	// Check the Identity certificate.
	c.Assert(idCert.Subject.String(), Equals, idTemplate.Subject.String())
	if !slices.Equal(idCert.DNSNames, idTemplate.DNSNames) {
		c.Fail()
	}
	if !slices.Equal(idCert.EmailAddresses, idTemplate.EmailAddresses) {
		c.Fail()
	}
}
