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

package daemon

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/canonical/tc"
)

// TestPairing checks that we can pair a client.
func (s *apiSuite) TestPairing(c *tc.C) {
	clientCert := createTestClientCertificate(c)

	pairingLayer := `
pairing:
    mode: single
`
	writeTestLayer(s.pebbleDir, pairingLayer)
	d := s.daemon(c)

	// Enable pairing window
	pairingMgr := d.overlord.PairingManager()
	err := pairingMgr.EnablePairing(10 * time.Second)
	c.Assert(err, tc.IsNil)

	pairingCmd := apiCmd("/v1/pairing")
	payload := bytes.NewBufferString(`{"action": "pair"}`)

	req, err := http.NewRequest("POST", "/v1/pairing", payload)
	c.Assert(err, tc.IsNil)
	req.TLS = &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{clientCert},
	}
	req = req.WithContext(context.WithValue(context.Background(), TransportTypeKey{}, TransportTypeHTTPS))

	rsp := v1PostPairing(pairingCmd, req, nil).(*resp)
	rec := httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)

	c.Check(rec.Code, tc.Equals, 200)
	c.Check(rsp.Status, tc.Equals, 200)
	c.Check(rsp.Type, tc.Equals, ResponseTypeSync)
	c.Check(rsp.Result, tc.IsNil)
}

// TestPairingPairManagerError checks that pairing fails if attemped without
// the pairing window enabled.
func (s *apiSuite) TestPairingPairManagerError(c *tc.C) {
	clientCert := createTestClientCertificate(c)

	_ = s.daemon(c)

	pairingCmd := apiCmd("/v1/pairing")
	payload := bytes.NewBufferString(`{"action": "pair"}`)

	req, err := http.NewRequest("POST", "/v1/pairing", payload)
	c.Assert(err, tc.IsNil)
	req.TLS = &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{clientCert},
	}
	req = req.WithContext(context.WithValue(context.Background(), TransportTypeKey{}, TransportTypeHTTPS))

	rsp := v1PostPairing(pairingCmd, req, nil).(*resp)
	rec := httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)

	c.Check(rec.Code, tc.Equals, 400)
	c.Check(rsp.Status, tc.Equals, 400)
	c.Check(rsp.Type, tc.Equals, ResponseTypeError)

	result, ok := rsp.Result.(*errorResult)
	c.Assert(ok, tc.Equals, true)
	c.Assert(result.Message, tc.Matches, `cannot pair client:.*`)
}

// TestPairingPairInvalidJSON verifies that an invalid json payload will
// be detected.
func (s *apiSuite) TestPairingPairInvalidJSON(c *tc.C) {
	pairingCmd := apiCmd("/v1/pairing")
	payload := bytes.NewBufferString(`invalid json`)

	req, err := http.NewRequest("POST", "/v1/pairing", payload)
	c.Assert(err, tc.IsNil)
	req = req.WithContext(context.WithValue(context.Background(), TransportTypeKey{}, TransportTypeHTTPS))

	rsp := v1PostPairing(pairingCmd, req, nil).(*resp)
	rec := httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)

	c.Check(rec.Code, tc.Equals, 400)
	c.Check(rsp.Status, tc.Equals, 400)
	c.Check(rsp.Type, tc.Equals, ResponseTypeError)

	result, ok := rsp.Result.(*errorResult)
	c.Assert(ok, tc.Equals, true)
	c.Assert(result.Message, tc.Matches, `cannot decode request body:.*`)
}

// TestPairingPairInvalidJSON verifies that an invalid action string in the
// payload will be detected.
func (s *apiSuite) TestPairingPairInvalidAction(c *tc.C) {
	pairingCmd := apiCmd("/v1/pairing")
	payload := bytes.NewBufferString(`{"action": "invalid"}`)

	req, err := http.NewRequest("POST", "/v1/pairing", payload)
	c.Assert(err, tc.IsNil)
	req = req.WithContext(context.WithValue(context.Background(), TransportTypeKey{}, TransportTypeHTTPS))

	rsp := v1PostPairing(pairingCmd, req, nil).(*resp)
	rec := httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)

	c.Check(rec.Code, tc.Equals, 400)
	c.Check(rsp.Status, tc.Equals, 400)
	c.Check(rsp.Type, tc.Equals, ResponseTypeError)

	result, ok := rsp.Result.(*errorResult)
	c.Assert(ok, tc.Equals, true)
	c.Assert(result.Message, tc.Equals, `invalid action "invalid", must be "pair"`)
}

// TestPairingPairNonHTTPS confirms that any non-HTTPS transport is not
// supported.
func (s *apiSuite) TestPairingPairNonHTTPS(c *tc.C) {
	pairingCmd := apiCmd("/v1/pairing")
	payload := bytes.NewBufferString(`{"action": "pair"}`)

	req, err := http.NewRequest("POST", "/v1/pairing", payload)
	c.Assert(err, tc.IsNil)
	req = req.WithContext(context.WithValue(context.Background(), TransportTypeKey{}, TransportTypeHTTP))

	rsp := v1PostPairing(pairingCmd, req, nil).(*resp)
	rec := httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)

	c.Check(rec.Code, tc.Equals, 500)
	c.Check(rsp.Status, tc.Equals, 500)
	c.Check(rsp.Type, tc.Equals, ResponseTypeError)

	result, ok := rsp.Result.(*errorResult)
	c.Assert(ok, tc.Equals, true)
	c.Assert(result.Message, tc.Equals, `cannot find TLS connection state`)
}

// TestPairingPairMissingTLSState verifies that missing TLS state
// will result in pairing failure.
func (s *apiSuite) TestPairingPairMissingTLSState(c *tc.C) {
	pairingCmd := apiCmd("/v1/pairing")
	payload := bytes.NewBufferString(`{"action": "pair"}`)

	req, err := http.NewRequest("POST", "/v1/pairing", payload)
	c.Assert(err, tc.IsNil)
	req = req.WithContext(context.WithValue(context.Background(), TransportTypeKey{}, TransportTypeHTTPS))

	rsp := v1PostPairing(pairingCmd, req, nil).(*resp)
	rec := httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)

	c.Check(rec.Code, tc.Equals, 500)
	c.Check(rsp.Status, tc.Equals, 500)
	c.Check(rsp.Type, tc.Equals, ResponseTypeError)

	result, ok := rsp.Result.(*errorResult)
	c.Assert(ok, tc.Equals, true)
	c.Assert(result.Message, tc.Equals, `cannot find TLS connection state`)
}

// TestPairingPairZeroPeerCertificates verifies that if the client does
// not supply exactly one certificate, we will not proceed with pairing.
func (s *apiSuite) TestPairingPairZeroPeerCertificates(c *tc.C) {
	pairingCmd := apiCmd("/v1/pairing")
	payload := bytes.NewBufferString(`{"action": "pair"}`)

	req, err := http.NewRequest("POST", "/v1/pairing", payload)
	c.Assert(err, tc.IsNil)
	req.TLS = &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{},
	}
	req = req.WithContext(context.WithValue(context.Background(), TransportTypeKey{}, TransportTypeHTTPS))

	rsp := v1PostPairing(pairingCmd, req, nil).(*resp)
	rec := httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)

	c.Check(rec.Code, tc.Equals, 400)
	c.Check(rsp.Status, tc.Equals, 400)
	c.Check(rsp.Type, tc.Equals, ResponseTypeError)

	result, ok := rsp.Result.(*errorResult)
	c.Assert(ok, tc.Equals, true)
	c.Assert(result.Message, tc.Equals, `cannot support client: single certificate expected, got 0`)
}
