// Copyright (c) 2024 Canonical Ltd
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

//go:build !fips

package client_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"time"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/client"
)

func (cs *clientSuite) TestClientIntegrationHTTPS(c *C) {
	clientTLSCerts := createTestClientTLSCerts(c)
	serverTLSCerts, serverIDCert, serverFingerprint := createTestServerTLSCerts(c)

	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		c.Assert(err, IsNil)
	}
	defer listener.Close()
	// Get the allocated port.
	testPort := listener.Addr().(*net.TCPAddr).Port

	handler := func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.URL.Path, Equals, "/v1/system-info")
		c.Check(r.URL.RawQuery, Equals, "")

		// Validate client identity cert.
		roots := x509.NewCertPool()
		roots.AddCert(clientTLSCerts.Leaf)
		opts := x509.VerifyOptions{
			Roots:     roots,
			KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		}
		incomingTLS := r.TLS.PeerCertificates[0]
		_, err := incomingTLS.Verify(opts)
		c.Assert(err, IsNil)

		fmt.Fprintln(w, `{"type":"sync", "result":{"version":"1"}}`)
	}

	srv := &httptest.Server{
		Listener: listener,
		Config: &http.Server{
			Handler: http.HandlerFunc(handler),
		},
		TLS: &tls.Config{
			NextProtos: []string{"h2", "http/1.1"},
			MinVersion: tls.VersionTLS13,
			ClientAuth: tls.RequestClientCert,
			GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
				return serverTLSCerts, nil
			},
		},
	}
	// StartTLS will generate a TLS keypair.
	srv.StartTLS()
	defer srv.Close()

	// 1. Client without TLSServerFingerprint or TLSServerIDCert should not
	// allow a HTTPS connection with the server.
	cli, err := client.New(&client.Config{
		BaseURL:         fmt.Sprintf("https://localhost:%d", testPort),
		TLSClientIDCert: clientTLSCerts,
	})
	c.Assert(err, IsNil)
	_, err = cli.SysInfo()
	c.Assert(err, ErrorMatches, ".*cannot verify server: see TLS config options")

	// 2. Client with TLSServerInsecure true should allow a HTTPS connection with the server.
	cli, err = client.New(&client.Config{
		BaseURL:           fmt.Sprintf("https://localhost:%d", testPort),
		TLSServerInsecure: true,
		TLSClientIDCert:   clientTLSCerts,
	})
	c.Assert(err, IsNil)

	cert, si, err := cli.SysInfoWithServerID()
	c.Check(err, IsNil)
	c.Check(si.Version, Equals, "1")
	c.Check(cert, DeepEquals, serverIDCert)

	// 3. Let's simulate a pairing attempt by supplying the server
	// fingerprint instead of the server identity certificate.
	//
	// Important: This test only tests the client side logic. The test
	// server in this case does not perform client identity lookup and
	// access checks.
	cli, err = client.New(&client.Config{
		BaseURL:              fmt.Sprintf("https://localhost:%d", testPort),
		TLSServerFingerprint: serverFingerprint,
		TLSClientIDCert:      clientTLSCerts,
	})
	c.Assert(err, IsNil)

	cert, si, err = cli.SysInfoWithServerID()
	c.Check(err, IsNil)
	c.Check(si.Version, Equals, "1")
	c.Check(cert, DeepEquals, serverIDCert)

	// 4. Let's simulate a normal TLS request by supplying the server
	// identity certificate.
	//
	// Important: This test only tests the client side logic. The test
	// server in this case does not perform client identity lookup and
	// access checks.
	cli, err = client.New(&client.Config{
		BaseURL:         fmt.Sprintf("https://localhost:%d", testPort),
		TLSServerIDCert: serverIDCert,
		TLSClientIDCert: clientTLSCerts,
	})
	c.Assert(err, IsNil)

	cert, si, err = cli.SysInfoWithServerID()
	c.Check(err, IsNil)
	c.Check(si.Version, Equals, "1")
	c.Check(cert, DeepEquals, serverIDCert)
}

func createTestServerTLSCerts(c *C) (*tls.Certificate, *x509.Certificate, string) {
	_, caKey, err := ed25519.GenerateKey(rand.Reader)
	c.Assert(err, IsNil)

	template := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{},
		BasicConstraintsValid: true,
		IsCA:                  true,
		// We can only sign leaf certificates with this.
		MaxPathLen:     0,
		MaxPathLenZero: true,
	}

	// Self-signed certificate.
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, caKey.Public(), caKey)
	c.Assert(err, IsNil)

	caCert, err := x509.ParseCertificate(certDER)
	c.Assert(err, IsNil)

	_, tlsKey, err := ed25519.GenerateKey(rand.Reader)
	c.Assert(err, IsNil)

	template = x509.Certificate{
		SerialNumber:          big.NewInt(1),
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	// CA signed TLS certificate.
	certDER, err = x509.CreateCertificate(rand.Reader, &template, caCert, tlsKey.Public(), caKey)
	c.Assert(err, IsNil)

	tlsCert, err := x509.ParseCertificate(certDER)
	c.Assert(err, IsNil)

	// Fingerprint
	fingerprint, err := client.GetIdentityFingerprint(caCert)
	c.Assert(err, IsNil)

	tlsConfig := &tls.Certificate{
		Certificate: [][]byte{tlsCert.Raw, caCert.Raw},
		PrivateKey:  tlsKey,
		Leaf:        tlsCert,
	}
	return tlsConfig, caCert, fingerprint
}

func createTestClientTLSCerts(c *C) *tls.Certificate {
	_, tlsKeyPair, err := ed25519.GenerateKey(rand.Reader)
	c.Assert(err, IsNil)

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}

	// Self-signed certificate.
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, tlsKeyPair.Public(), tlsKeyPair)
	c.Assert(err, IsNil)

	cert, err := x509.ParseCertificate(certDER)
	c.Assert(err, IsNil)

	return &tls.Certificate{
		Certificate: [][]byte{cert.Raw},
		PrivateKey:  tlsKeyPair,
		Leaf:        cert,
	}
}
