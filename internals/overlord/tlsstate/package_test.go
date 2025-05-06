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
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	. "gopkg.in/check.v1"
	//	"github.com/canonical/pebble/internals/overlord/tlsstate"
)

// Hook up check.v1 into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }

type tlsSuite struct{}

var _ = Suite(&tlsSuite{})

// GetFakeTime converts a simple string date to the time.Time type for testing purposes.
func (ts *tlsSuite) GetFakeTime(c *C, date string) time.Time {
	layout := "2006-01-02"
	now, err := time.Parse(layout, date)
	c.Assert(err, IsNil)
	return now
}

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
				MaxVersion:         tls.VersionTLS13,
				InsecureSkipVerify: true,
				VerifyConnection: func(ts tls.ConnectionState) error {
					serverCerts = ts.PeerCertificates
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
	response, err := client.Get("https://localhost:8888")
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
	listener, err := net.Listen("tcp", ":8888")
	if err != nil {
		c.Assert(err, IsNil)
	}

	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, "TLS 1.3!")
		}),
		TLSConfig: &tls.Config{
			MinVersion:     tls.VersionTLS13,
			MaxVersion:     tls.VersionTLS13,
			GetCertificate: getCertificate,
		},
	}
	go func() {
		err := server.ServeTLS(listener, "", "")
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			c.Fatalf("Cannot start server: %v", err)
		}
	}()

	// The server must be shutdown when client tests are completed.
	return func() {
		server.Shutdown(context.Background())
		listener.Close()
	}
}
