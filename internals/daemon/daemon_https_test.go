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

package daemon

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"time"

	. "gopkg.in/check.v1"
)

func (s *daemonSuite) TestHTTPSOpenAccess(c *C) {
	s.httpsAddress = ":0" // Go will choose port (use listener.Addr() to find it)
	d := s.newDaemon(c)
	d.Init()
	c.Assert(d.Start(), IsNil)

	cleanupServer := true
	defer func() {
		// If we exit early (test failure), clean up.
		if cleanupServer {
			d.Stop(nil)
		}
	}()

	port := d.httpsListener.Addr().(*net.TCPAddr).Port

	clientTLS := createTestClientTLSKeypair(c)
	httpsClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
				GetClientCertificate: func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
					return clientTLS, nil
				},
			},
		},
	}

	request, err := http.NewRequest("GET", fmt.Sprintf("https://localhost:%d/v1/health", port), nil)
	c.Assert(err, IsNil)
	response, err := httpsClient.Do(request)
	c.Assert(err, IsNil)
	c.Assert(response.StatusCode, Equals, http.StatusOK)
	var m map[string]any
	err = json.NewDecoder(response.Body).Decode(&m)
	c.Assert(err, IsNil)
	c.Assert(m, DeepEquals, map[string]any{
		"type":        "sync",
		"status-code": float64(http.StatusOK),
		"status":      "OK",
		"result": map[string]any{
			"healthy": true,
		},
	})

	err = d.Stop(nil)
	c.Assert(err, IsNil)

	// Daemon already stopped, no need to do it again during defer.
	cleanupServer = false

	_, err = http.DefaultClient.Do(request)
	c.Assert(err, ErrorMatches, ".* connection refused")
}

func (s *daemonSuite) TestHTTPSUserAccessFail(c *C) {
	s.httpsAddress = ":0" // Go will choose port (use listener.Addr() to find it)
	d := s.newDaemon(c)
	d.Init()
	c.Assert(d.Start(), IsNil)

	cleanupServer := true
	defer func() {
		// If we exit early (test failure), clean up.
		if cleanupServer {
			d.Stop(nil)
		}
	}()

	port := d.httpsListener.Addr().(*net.TCPAddr).Port

	clientTLS := createTestClientTLSKeypair(c)
	httpsClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
				GetClientCertificate: func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
					return clientTLS, nil
				},
			},
		},
	}

	request, err := http.NewRequest("GET", fmt.Sprintf("https://localhost:%d/v1/checks", port), nil)
	c.Assert(err, IsNil)
	response, err := httpsClient.Do(request)
	c.Assert(err, IsNil)

	// Access fails because the TLS client identity is not known.
	c.Assert(response.StatusCode, Equals, http.StatusUnauthorized)

	err = d.Stop(nil)
	c.Assert(err, IsNil)

	// Daemon already stopped, no need to do it again during defer.
	cleanupServer = false

	_, err = http.DefaultClient.Do(request)
	c.Assert(err, ErrorMatches, ".* connection refused")
}

func (s *daemonSuite) TestHTTPSUserAccessOK(c *C) {
	s.httpsAddress = ":0" // Go will choose port (use listener.Addr() to find it)

	// Write pairing configuration layer
	pairingLayer := `
pairing:
    mode: single
`
	writeTestLayer(s.pebbleDir, pairingLayer)

	d := s.newDaemon(c)
	d.Init()
	c.Assert(d.Start(), IsNil)

	cleanupServer := true
	defer func() {
		// If we exit early (test failure), clean up.
		if cleanupServer {
			d.Stop(nil)
		}
	}()

	port := d.httpsListener.Addr().(*net.TCPAddr).Port

	clientTLS := createTestClientTLSKeypair(c)
	httpsClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
				GetClientCertificate: func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
					return clientTLS, nil
				},
			},
		},
	}

	// Enable pairing window
	pairingMgr := d.overlord.PairingManager()
	err := pairingMgr.EnablePairing(10 * time.Second)
	c.Assert(err, IsNil)

	// Pair the client identity by making a POST to v1/pairing API
	pairingPayload := bytes.NewBufferString(`{"action": "pair"}`)
	pairingRequest, err := http.NewRequest("POST", fmt.Sprintf("https://localhost:%d/v1/pairing", port), pairingPayload)
	c.Assert(err, IsNil)
	pairingResponse, err := httpsClient.Do(pairingRequest)
	c.Assert(err, IsNil)
	c.Assert(pairingResponse.StatusCode, Equals, http.StatusOK)

	// Now try accessing checks - should succeed
	request, err := http.NewRequest("GET", fmt.Sprintf("https://localhost:%d/v1/checks", port), nil)
	c.Assert(err, IsNil)
	response, err := httpsClient.Do(request)
	c.Assert(err, IsNil)

	// Access succeeds because the TLS client identity is now paired
	c.Assert(response.StatusCode, Equals, http.StatusOK)

	err = d.Stop(nil)
	c.Assert(err, IsNil)

	// Daemon already stopped, no need to do it again during defer.
	cleanupServer = false

	_, err = http.DefaultClient.Do(request)
	c.Assert(err, ErrorMatches, ".* connection refused")
}

func createTestClientTLSKeypair(c *C) *tls.Certificate {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	c.Assert(err, IsNil)

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}

	// Self-signed certificate.
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, privateKey.Public(), privateKey)
	c.Assert(err, IsNil)

	cert, err := x509.ParseCertificate(certDER)
	c.Assert(err, IsNil)

	return &tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  privateKey,
		Leaf:        cert,
	}
}
