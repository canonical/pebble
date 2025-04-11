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
	"fmt"
	"os"
	"path/filepath"
	"time"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/overlord/tlsstate"
)

// TLS manager should successfully start up even if the supplied TLS
// directory does not exist.
func (ts *tlsSuite) TestNoDirectory(c *C) {
	tlsDir := filepath.Join(c.MkDir(), "tls")
	_, err := tlsstate.NewManager(tlsDir)
	c.Assert(err, IsNil)
}

// TLS manager should fail to start up if the TLS directory does not
// have the correct permissions.
func (ts *tlsSuite) TestDirectoryInvalid(c *C) {
	tlsDir := filepath.Join(c.MkDir(), "tls")
	err := os.MkdirAll(tlsDir, 0740)
	c.Assert(err, IsNil)
	_, err = tlsstate.NewManager(tlsDir)
	c.Assert(err, ErrorMatches, ".* expected permission 0o700 .*")
}

// TLS manager should fail if the parent of the tls directory does not
// exist.
func (ts *tlsSuite) TestKeypairDirInvalid(c *C) {
	tlsDir := filepath.Join(c.MkDir(), "something/tls")
	mgr, err := tlsstate.NewManager(tlsDir)
	c.Assert(err, IsNil)
	_, err = mgr.TLSKeyPair()
	c.Assert(err, ErrorMatches, "cannot create directory leaf .*")
}

// TLS manager should fail to start up if the TLS directory includes
// a keypair with invalid permissions.
func (ts *tlsSuite) TestKeyPairPermission(c *C) {
	tlsDir := filepath.Join(c.MkDir(), "tls")
	err := os.MkdirAll(tlsDir, 0700)
	c.Assert(err, IsNil)
	keypair := ts.createSupportedKeypair(c, tlsDir, time.Now(), time.Hour)
	err = os.Chmod(filepath.Join(tlsDir, fmt.Sprintf("%v.pem", keypair.Fingerprint())), 0644)
	c.Assert(err, IsNil)
	_, err = tlsstate.NewManager(tlsDir)
	c.Assert(err, ErrorMatches, "cannot verify PEM permission .*")
}

// TLS manager should fail to start up if the TLS directory includes
// a keypair with invalid PEM data.
func (ts *tlsSuite) TestInvalidKeypairFile(c *C) {
	tlsDir := filepath.Join(c.MkDir(), "tls")
	err := os.MkdirAll(tlsDir, 0700)
	c.Assert(err, IsNil)
	keypair := ts.createSupportedKeypair(c, tlsDir, time.Now(), time.Hour)
	// Zero the existing file.
	f, err := os.OpenFile(filepath.Join(tlsDir, fmt.Sprintf("%v.pem", keypair.Fingerprint())), os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600)
	c.Assert(err, IsNil)
	err = f.Close()
	c.Assert(err, IsNil)
	_, err = tlsstate.NewManager(tlsDir)
	c.Assert(err, ErrorMatches, "cannot load keypair from PEM file .*")
}

// TLS Manager should load the content of the tls directory, picking up all
// the PEM files, and then order them according to the start date and then
// by end date.
func (ts *tlsSuite) TestAllLoaded(c *C) {
	tlsDir := filepath.Join(c.MkDir(), "tls")
	err := os.MkdirAll(tlsDir, 0700)
	c.Assert(err, IsNil)

	// The test runs with the clock set to exactly this point in time.
	timeBaseString := "2000-01-01"
	timeBase := ts.GetFakeTime(c, timeBaseString)

	day := 24 * time.Hour
	year := 365 * day

	tests := []struct {
		startDelta time.Duration
		duration   time.Duration
		supported  bool
		expired    bool
		keypair    *tlsstate.TLSKeyPair
	}{{
		// 0
		startDelta: year,
		duration:   year,
		supported:  true,
		expired:    true,
	}, {
		// 1
		startDelta: year,
		duration:   10 * year,
		supported:  false,
		expired:    true,
	}, {
		// 2
		startDelta: day,
		duration:   year,
		supported:  true,
		expired:    true,
	}, {
		// 3
		startDelta: -day,
		duration:   year,
		supported:  true,
		expired:    false,
	}, {
		// 4
		startDelta: -day,
		duration:   day,
		supported:  false,
		expired:    false,
	}, {
		// 5
		startDelta: year,
		duration:   day,
		supported:  true,
		expired:    true,
	}}

	// Install the PEM files before we start up the manager.
	for i, test := range tests {
		if test.supported {
			tests[i].keypair = ts.createSupportedKeypair(c, tlsDir, timeBase.Add(test.startDelta), test.duration)
		} else {
			tests[i].keypair = ts.createUnsupportedKeypair(c, tlsDir, timeBase.Add(test.startDelta), test.duration)
		}
	}

	mgr, err := tlsstate.NewManager(tlsDir)
	c.Assert(err, IsNil)

	restore, _ := mgr.FakeSystemTime(timeBaseString, 0)
	defer restore()

	// The result should be ordered.
	expectedOrder := []int{
		4,
		3,
		2,
		5,
		0,
		1,
	}

	keypairs := mgr.TLSKeyPairsAll()
	c.Assert(keypairs, HasLen, len(expectedOrder))

	for i, testOrder := range expectedOrder {
		c.Assert(keypairs[i].Fingerprint(), Equals, tests[testOrder].keypair.Fingerprint())
		c.Assert(keypairs[i].IsExpired(), Equals, tests[testOrder].expired)
		c.Assert(mgr.IsTLSKeypairSupported(keypairs[i]), Equals, tests[testOrder].supported)
	}
}

// TestBenchmarkTLSCreate checks how long it takes to generate a new TLS
// keypair on demand. Run with "go test -check.vv" to see output.
func (ts *tlsSuite) TestBenchmarkTLSCreate(c *C) {
	tlsDir := filepath.Join(c.MkDir(), "tls")
	mgr, err := tlsstate.NewManager(tlsDir)
	c.Assert(err, IsNil)

	// New certificates are valid for 1 year.
	year := 365 * 24 * time.Hour
	restoreValidityPeriod := mgr.FakeCertificateValidityPeriod(year)
	defer restoreValidityPeriod()

	start := time.Now()
	runs := 100
	for i := 0; i < runs; i++ {
		// Move the clock forward on every iteration so we force the previous
		// certificate to expire (expiry time + 1 hour). This ensures we generate
		// a new TLS keypair on the next request.
		restore, _ := mgr.FakeSystemTime("2000-01-01", time.Duration(i)*(year+time.Hour))
		_, err := mgr.TLSKeyPair()
		c.Assert(err, IsNil)
		restore()
	}
	elapse := time.Since(start)
	c.Logf("TLS keypair issue time: %v", elapse/100)

	keypairs := mgr.TLSKeyPairsAll()
	c.Assert(len(keypairs), Equals, runs)
}

// TestTLSServerClientEmpty gets a TLS HTTPS server running with an empty TLS
// directory as starting point. This test ensures TLS keypair generation works
// on demand, starting with nothing.
func (ts *tlsSuite) TestTLSServerClientEmpty(c *C) {
	tlsDir := filepath.Join(c.MkDir(), "tls")
	mgr, err := tlsstate.NewManager(tlsDir)
	c.Assert(err, IsNil)

	// New certificates are valid for just under 1 year.
	year := 364 * 24 * time.Hour
	restoreValidityPeriod := mgr.FakeCertificateValidityPeriod(year)
	defer restoreValidityPeriod()

	// Start the HTTPS server.
	shutdown := ts.testTLSServer(c, func() *tlsstate.TLSKeyPair {
		// Get a TLS certificate when needed.
		keypair, err := mgr.TLSKeyPair()
		c.Assert(err, IsNil)
		return keypair
	})
	defer shutdown()

	// The date on which the client tries to contact the server.
	httpsSessions := []struct {
		date          string
		totalKeyPairs int
	}{{
		date:          "2000-01-01",
		totalKeyPairs: 1,
	}, {
		date:          "2000-02-01",
		totalKeyPairs: 1,
	}, {
		date:          "2003-01-01",
		totalKeyPairs: 2,
	}, {
		date:          "2004-01-01",
		totalKeyPairs: 3,
	}, {
		date:          "2004-01-09",
		totalKeyPairs: 3,
	}}

	for _, session := range httpsSessions {
		restore, clock := mgr.FakeSystemTime(session.date, 0)

		// Test an untrusted client connection.
		serverCert, err := ts.testTLSInsecureClient(c, clock)
		c.Assert(err, IsNil)

		// Test a trusted client connection (we use the self-signed TLS
		// certificate as the root CA).
		_, err = ts.testTLSVerifiedClient(c, serverCert, clock)
		c.Assert(err, IsNil)

		// All keypairs.
		keypairs := mgr.TLSKeyPairsAll()
		c.Assert(keypairs, HasLen, session.totalKeyPairs)
		// Supported / not-expired keypairs with a given date. In this
		// test there should only ever be a single keypair for every
		// test date.
		keypairs = mgr.TLSKeyPairs()
		c.Assert(keypairs, HasLen, 1)

		restore()
	}
}

// TestTLSServerClientSeeded gets a TLS HTTPS server running with an non-empty TLS
// directory as starting point. This test ensures TLS keypair generation works
// on demand, and that existing TLS keypairs are taken into account.
func (ts *tlsSuite) TestTLSServerClientSeeded(c *C) {
	tlsDir := filepath.Join(c.MkDir(), "tls")

	// Let's install some TLS keypairs before the manager starts up.
	installedTLSKeypairs := []struct {
		start     time.Time
		duration  time.Duration
		supported bool
		expired   bool
		keypair   *tlsstate.TLSKeyPair
	}{{
		start:    ts.GetFakeTime(c, "2003-01-01"),
		duration: 24 * time.Hour,
	}, {
		start:    ts.GetFakeTime(c, "2004-01-01"),
		duration: 24 * time.Hour,
	}}
	for _, keypair := range installedTLSKeypairs {
		ts.createSupportedKeypair(c, tlsDir, keypair.start, keypair.duration)
	}

	// Start up the manager.
	mgr, err := tlsstate.NewManager(tlsDir)
	c.Assert(err, IsNil)

	// New certificates are valid for just under 1 year.
	year := 364 * 24 * time.Hour
	restoreValidityPeriod := mgr.FakeCertificateValidityPeriod(year)
	defer restoreValidityPeriod()

	// Start the HTTPS server.
	shutdown := ts.testTLSServer(c, func() *tlsstate.TLSKeyPair {
		// Get a TLS certificate when needed.
		keypair, err := mgr.TLSKeyPair()
		c.Assert(err, IsNil)
		return keypair
	})
	defer shutdown()

	// The date on which the client tries to contact the server.
	httpsSessions := []struct {
		date          string
		totalKeyPairs int
	}{{
		date:          "2000-01-01",
		totalKeyPairs: 3,
	}, {
		date:          "2000-02-01",
		totalKeyPairs: 3,
	}, {
		date:          "2003-01-01",
		totalKeyPairs: 3,
	}, {
		date:          "2004-01-01",
		totalKeyPairs: 3,
	}, {
		date:          "2004-01-09",
		totalKeyPairs: 4,
	}}

	for _, session := range httpsSessions {
		restore, clock := mgr.FakeSystemTime(session.date, 0)

		// Test an untrusted client connection.
		serverCert, err := ts.testTLSInsecureClient(c, clock)
		c.Assert(err, IsNil)

		// Test a trusted client connection (we use the self-signed TLS
		// certificate as the root CA).
		_, err = ts.testTLSVerifiedClient(c, serverCert, clock)
		c.Assert(err, IsNil)

		// All keypairs.
		keypairs := mgr.TLSKeyPairsAll()
		c.Assert(keypairs, HasLen, session.totalKeyPairs)
		// Supported / not-expired keypairs with a given date. In this
		// test there should only ever be a single keypair for every
		// test date.
		keypairs = mgr.TLSKeyPairs()
		c.Assert(keypairs, HasLen, 1)

		restore()
	}
}
