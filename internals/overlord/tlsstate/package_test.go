// Copyright (C) 2025 Canonical Ltd
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
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha512"
	"crypto/tls"
	"crypto/x509"
	"encoding/base32"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	. "gopkg.in/check.v1"
)

// Hook up check.v1 into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }

type tlsSuite struct {
	// The HTTPS server port number allocated during a test.
	serverHTTPSPort int
}

var _ = Suite(&tlsSuite{})

// testTLSVerifiedClient performs a client TLS connection without server certificate
// signature verification.
func (ts *tlsSuite) testTLSInsecureClient(c *C, clock time.Time) ([]*x509.Certificate, error) {
	return ts.testTLSClient(c, nil, clock)
}

// testTLSVerifiedClient performs a client TLS connection with server certificate
// signature verification.
func (ts *tlsSuite) testTLSVerifiedClient(c *C, ca *x509.Certificate, clock time.Time) ([]*x509.Certificate, error) {
	return ts.testTLSClient(c, ca, clock)
}

func (ts *tlsSuite) testTLSClient(c *C, ca *x509.Certificate, clock time.Time) ([]*x509.Certificate, error) {
	var serverCerts []*x509.Certificate

	certPool := x509.NewCertPool()
	if ca != nil {
		certPool.AddCert(ca)
	}
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				MinVersion:         tls.VersionTLS13,
				InsecureSkipVerify: true,
				VerifyConnection: func(cs tls.ConnectionState) error {
					serverCerts = cs.PeerCertificates
					if len(serverCerts) == 0 {
						return fmt.Errorf("no server cert")
					}
					if ca == nil {
						// No verification.
						return nil
					}
					opts := x509.VerifyOptions{
						Roots:       certPool,
						CurrentTime: clock,
					}
					serverLeaf := serverCerts[0]
					_, err := serverLeaf.Verify(opts)
					if err != nil {
						return fmt.Errorf("Root CA verify failed")
					}
					return nil
				},
			},
		},
	}
	response, err := client.Get(fmt.Sprintf("https://localhost:%d", ts.serverHTTPSPort))
	if err != nil {
		// We want to monitor this error for some tests.
		return nil, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	c.Assert(err, IsNil)
	c.Assert(string(body), Equals, "TLS 1.3!")

	return serverCerts, nil
}

func (ts *tlsSuite) testTLSServer(c *C, getCertificate func(*tls.ClientHelloInfo) (*tls.Certificate, error)) (shutdown func()) {
	// First start a listener so we buffer client requests while the HTTPS
	// server routine is starting up.
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		c.Assert(err, IsNil)
	}
	// Get the allocated port.
	ts.serverHTTPSPort = listener.Addr().(*net.TCPAddr).Port

	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, "TLS 1.3!")
		}),
		TLSConfig: &tls.Config{
			MinVersion:     tls.VersionTLS13,
			GetCertificate: getCertificate,
		},
	}

	exitCh := make(chan struct{})
	go func() {
		err := server.ServeTLS(listener, "", "")
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			// Log an error message.
			c.Logf("HTTPS server returned an error: %v", err)
			// Mark the test as failed.
			c.Fail()
		}
		close(exitCh)
	}()

	// The server must be shutdown when client tests are completed.
	return func() {
		server.Shutdown(context.Background())
		// Wait for server goroutine to complete.
		<-exitCh
		listener.Close()
	}
}

// getTestTime helps generate a time for testing purposes.
func getTestTime(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

type idkey struct {
	ed25519.PrivateKey
}

func newIDKey(c *C) *idkey {
	k := &idkey{}
	var err error
	_, k.PrivateKey, err = ed25519.GenerateKey(rand.Reader)
	c.Assert(err, IsNil)
	return k
}

func (k *idkey) Fingerprint() string {
	publicBytes := k.PrivateKey.Public().(ed25519.PublicKey)
	hashBytes := sha512.Sum384(publicBytes)
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(hashBytes[:])
}
