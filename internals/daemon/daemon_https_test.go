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

	"github.com/canonical/tc"
)

func (s *daemonSuite) TestHTTPSOpenAccess(c *tc.C) {
	s.httpsAddress = ":0" // Go will choose port (use listener.Addr() to find it)
	d := s.newDaemon(c)
	d.Init()
	c.Assert(d.Start(), tc.IsNil)

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
	c.Assert(err, tc.ErrorIsNil)
	response, err := httpsClient.Do(request)
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(response.StatusCode, tc.Equals, http.StatusOK)
	var m map[string]any
	err = json.NewDecoder(response.Body).Decode(&m)
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(m, tc.DeepEquals, map[string]any{
		"type":        "sync",
		"status-code": float64(http.StatusOK),
		"status":      "OK",
		"result": map[string]any{
			"healthy": true,
		},
	})

	err = d.Stop(nil)
	c.Assert(err, tc.ErrorIsNil)

	// Daemon already stopped, no need to do it again during defer.
	cleanupServer = false

	_, err = http.DefaultClient.Do(request)
	c.Assert(err, tc.ErrorMatches, ".* connection refused")
}

func (s *daemonSuite) TestHTTPSUserAccessFail(c *tc.C) {
	s.httpsAddress = ":0" // Go will choose port (use listener.Addr() to find it)
	d := s.newDaemon(c)
	d.Init()
	c.Assert(d.Start(), tc.IsNil)

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
	c.Assert(err, tc.ErrorIsNil)
	response, err := httpsClient.Do(request)
	c.Assert(err, tc.ErrorIsNil)

	// Access fails because the TLS client identity is not known.
	c.Assert(response.StatusCode, tc.Equals, http.StatusUnauthorized)

	err = d.Stop(nil)
	c.Assert(err, tc.ErrorIsNil)

	// Daemon already stopped, no need to do it again during defer.
	cleanupServer = false

	_, err = http.DefaultClient.Do(request)
	c.Assert(err, tc.ErrorMatches, ".* connection refused")
}

func (s *daemonSuite) TestHTTPSUserAccessOK(c *tc.C) {
	s.httpsAddress = ":0" // Go will choose port (use listener.Addr() to find it)

	// Write pairing configuration layer
	pairingLayer := `
pairing:
    mode: single
`
	writeTestLayer(s.pebbleDir, pairingLayer)

	d := s.newDaemon(c)
	d.Init()
	c.Assert(d.Start(), tc.IsNil)

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
	c.Assert(err, tc.ErrorIsNil)

	// Pair the client identity by making a POST to v1/pairing API
	pairingPayload := bytes.NewBufferString(`{"action": "pair"}`)
	pairingRequest, err := http.NewRequest("POST", fmt.Sprintf("https://localhost:%d/v1/pairing", port), pairingPayload)
	c.Assert(err, tc.ErrorIsNil)
	pairingResponse, err := httpsClient.Do(pairingRequest)
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(pairingResponse.StatusCode, tc.Equals, http.StatusOK)

	// Now try accessing checks - should succeed
	request, err := http.NewRequest("GET", fmt.Sprintf("https://localhost:%d/v1/checks", port), nil)
	c.Assert(err, tc.ErrorIsNil)
	response, err := httpsClient.Do(request)
	c.Assert(err, tc.ErrorIsNil)

	// Access succeeds because the TLS client identity is now paired
	c.Assert(response.StatusCode, tc.Equals, http.StatusOK)

	err = d.Stop(nil)
	c.Assert(err, tc.ErrorIsNil)

	// Daemon already stopped, no need to do it again during defer.
	cleanupServer = false

	_, err = http.DefaultClient.Do(request)
	c.Assert(err, tc.ErrorMatches, ".* connection refused")
}

func createTestClientTLSKeypair(c *tc.C) *tls.Certificate {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	c.Assert(err, tc.ErrorIsNil)

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}

	// Self-signed certificate.
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, privateKey.Public(), privateKey)
	c.Assert(err, tc.ErrorIsNil)

	cert, err := x509.ParseCertificate(certDER)
	c.Assert(err, tc.ErrorIsNil)

	return &tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  privateKey,
		Leaf:        cert,
	}
}
