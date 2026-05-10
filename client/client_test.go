// Copyright (c) 2014-2020 Canonical Ltd
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

package client_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/canonical/tc"

	"github.com/canonical/pebble/client"
)

type clientSuite struct {
	cli          *client.Client
	req          *http.Request
	reqs         []*http.Request
	serverIdCert *x509.Certificate
	rsp          string
	rsps         []string
	err          error
	doCalls      int
	header       http.Header
	status       int
	tmpDir       string
	socketPath   string
	restore      func()
}

func TestClientSuite(t *testing.T) {
	tc.Run(t, &clientSuite{})
}

func (cs *clientSuite) SetUpTest(c *tc.C) {
	var err error
	cs.cli, err = client.New(nil)
	c.Assert(err, tc.ErrorIsNil)
	cs.cli.SetDoer(cs)
	cs.err = nil
	cs.req = nil
	cs.reqs = nil
	cs.rsp = ""
	cs.rsps = nil
	cs.req = nil
	cs.header = nil
	cs.status = 200
	cs.doCalls = 0

	cs.tmpDir = c.MkDir()
	cs.socketPath = filepath.Join(cs.tmpDir, "pebble.socket")

	cs.restore = client.FakeDoRetry(time.Millisecond, 10*time.Millisecond)
}

func (cs *clientSuite) TearDownTest(c *tc.C) {
	cs.restore()
}

// FakeTLSServer results in the inclusion of TLS certificates in the
// HTTP response.
func (cs *clientSuite) FakeTLSServer(idCert *x509.Certificate) {
	cs.serverIdCert = idCert
}

func (cs *clientSuite) Do(req *http.Request) (*http.Response, error) {
	cs.req = req
	cs.reqs = append(cs.reqs, req)
	body := cs.rsp
	if cs.doCalls < len(cs.rsps) {
		body = cs.rsps[cs.doCalls]
	}
	rsp := &http.Response{
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     cs.header,
		StatusCode: cs.status,
	}
	if cs.serverIdCert != nil {
		// Pretend this is a HTTPS connection.
		rsp.TLS = &tls.ConnectionState{
			PeerCertificates: []*x509.Certificate{
				// TLS certificate.
				{},
				// ID Certificate.
				cs.serverIdCert,
			},
		}
	}
	cs.doCalls++
	return rsp, cs.err
}

func (cs *clientSuite) TestNewBaseURLError(c *tc.C) {
	_, err := client.New(&client.Config{BaseURL: ":"})
	c.Assert(err, tc.ErrorMatches, `cannot parse base URL: parse ":": missing protocol scheme`)
}

func (cs *clientSuite) TestClientDoReportsErrors(c *tc.C) {
	cs.err = errors.New("ouchie")
	_, err := cs.cli.Requester().Do(context.Background(), &client.RequestOptions{
		Type:   client.RawRequest,
		Method: "GET",
		Path:   "/",
	})
	c.Assert(err, tc.NotNil)
	c.Check(err, tc.ErrorMatches, "cannot communicate with server: ouchie")
	if cs.doCalls < 2 {
		c.Fatalf("do did not retry")
	}
}

func (cs *clientSuite) TestContextCancellation(c *tc.C) {
	cs.err = errors.New("ouchie")
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel it right away
	_, err := cs.cli.Requester().Do(ctx, &client.RequestOptions{
		Type:   client.SyncRequest,
		Method: "GET",
		Path:   "/",
	})
	c.Check(err, tc.ErrorMatches, "cannot communicate with server: ouchie")

	// This would be 10 if context wasn't respected, due to timeout
	c.Assert(cs.doCalls, tc.Equals, 1)
}

func (cs *clientSuite) TestClientWorks(c *tc.C) {
	var v []int
	cs.rsp = `[1,2]`
	reqBody := io.NopCloser(strings.NewReader(""))
	resp, err := cs.cli.Requester().Do(context.Background(), &client.RequestOptions{
		Type:   client.RawRequest,
		Method: "GET",
		Path:   "/this",
		Body:   reqBody,
	})
	c.Assert(err, tc.ErrorIsNil)
	dec := json.NewDecoder(resp.Body)
	err = dec.Decode(&v)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(v, tc.DeepEquals, []int{1, 2})
	c.Assert(cs.req, tc.NotNil)
	c.Assert(cs.req.URL, tc.NotNil)
	c.Check(cs.req.Method, tc.Equals, "GET")
	c.Check(cs.req.Body, tc.Equals, reqBody)
	c.Check(cs.req.URL.Path, tc.Equals, "/this")
}

func (cs *clientSuite) TestClientDefaultsToNoAuthorization(c *tc.C) {
	_, _ = cs.cli.Requester().Do(context.Background(), &client.RequestOptions{
		Type:   client.RawRequest,
		Method: "GET",
		Path:   "/this",
	})
	c.Assert(cs.req, tc.NotNil)
	authorization := cs.req.Header.Get("Authorization")
	c.Check(authorization, tc.Equals, "")
}

func (cs *clientSuite) TestClientSysInfo(c *tc.C) {
	cs.rsp = `{"type": "sync", "result": {"version": "1"}}`
	sysInfo, err := cs.cli.SysInfo()
	c.Assert(err, tc.ErrorIsNil)
	c.Check(sysInfo, tc.DeepEquals, &client.SysInfo{Version: "1"})
}

func (cs *clientSuite) TestClientReportsOpError(c *tc.C) {
	cs.rsp = `{"type": "error", "status": "potatoes"}`
	_, err := cs.cli.SysInfo()
	c.Check(err, tc.ErrorMatches, `.*server error: "potatoes"`)
}

func (cs *clientSuite) TestClientReportsOpErrorStr(c *tc.C) {
	cs.rsp = `{
		"result": {},
		"status": "Bad Request",
		"status-code": 400,
		"type": "error"
	}`
	_, err := cs.cli.SysInfo()
	c.Check(err, tc.ErrorMatches, `.*server error: "Bad Request"`)
}

func (cs *clientSuite) TestClientReportsBadType(c *tc.C) {
	cs.rsp = `{"type": "what"}`
	_, err := cs.cli.SysInfo()
	c.Check(err, tc.ErrorMatches, `.*expected sync response, got "what"`)
}

func (cs *clientSuite) TestClientReportsOuterJSONError(c *tc.C) {
	cs.rsp = "this isn't really json is it"
	_, err := cs.cli.SysInfo()
	c.Check(err, tc.ErrorMatches, `.*invalid character .*`)
}

func (cs *clientSuite) TestClientReportsInnerJSONError(c *tc.C) {
	cs.rsp = `{"type": "sync", "result": "this isn't really json is it"}`
	_, err := cs.cli.SysInfo()
	c.Check(err, tc.ErrorMatches, `.*cannot unmarshal.*`)
}

func (cs *clientSuite) TestClientAsync(c *tc.C) {
	cs.rsp = `{"type":"async", "status-code": 202, "change": "42"}`
	changeId, err := cs.cli.FakeAsyncRequest()
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(changeId, tc.Equals, "42")
}

func (cs *clientSuite) TestClientMaintenance(c *tc.C) {
	cs.rsp = `{"type":"sync", "result":{"series":"42"}, "maintenance": {"kind": "system-restart", "message": "system is restarting"}}`
	_, err := cs.cli.SysInfo()
	c.Assert(err, tc.ErrorIsNil)
	c.Check(cs.cli.Maintenance().(*client.Error), tc.DeepEquals, &client.Error{
		Kind:    client.ErrorKindSystemRestart,
		Message: "system is restarting",
	})

	cs.rsp = `{"type":"sync", "result":{"series":"42"}}`
	_, err = cs.cli.SysInfo()
	c.Assert(err, tc.ErrorIsNil)
	c.Check(cs.cli.Maintenance(), tc.Equals, error(nil))
}

func (cs *clientSuite) TestClientAsyncOpMaintenance(c *tc.C) {
	cs.rsp = `{"type":"async", "status-code": 202, "change": "42", "maintenance": {"kind": "system-restart", "message": "system is restarting"}}`
	_, err := cs.cli.FakeAsyncRequest()
	c.Assert(err, tc.ErrorIsNil)
	c.Check(cs.cli.Maintenance().(*client.Error), tc.DeepEquals, &client.Error{
		Kind:    client.ErrorKindSystemRestart,
		Message: "system is restarting",
	})

	cs.rsp = `{"type":"async", "status-code": 202, "change": "42"}`
	_, err = cs.cli.FakeAsyncRequest()
	c.Assert(err, tc.ErrorIsNil)
	c.Check(cs.cli.Maintenance(), tc.Equals, error(nil))
}

func (cs *clientSuite) TestParseError(c *tc.C) {
	resp := &http.Response{
		Status: "404 tc.Not Found",
	}
	err := client.ParseErrorInTest(resp)
	c.Check(err, tc.ErrorMatches, `server error: "404 tc.Not Found"`)

	h := http.Header{}
	h.Add("Content-Type", "application/json")
	resp = &http.Response{
		Status: "400 Bad Request",
		Header: h,
		Body: io.NopCloser(strings.NewReader(`{
			"status-code": 400,
			"type": "error",
			"result": {
				"message": "invalid"
			}
		}`)),
	}
	err = client.ParseErrorInTest(resp)
	c.Check(err, tc.ErrorMatches, "invalid")

	resp = &http.Response{
		Status: "400 Bad Request",
		Header: h,
		Body:   io.NopCloser(strings.NewReader("{}")),
	}
	err = client.ParseErrorInTest(resp)
	c.Check(err, tc.ErrorMatches, `server error: "400 Bad Request"`)
}

func (cs *clientSuite) TestUserAgent(c *tc.C) {
	cli, err := client.New(&client.Config{UserAgent: "some-agent/9.87"})
	c.Assert(err, tc.ErrorIsNil)
	cli.SetDoer(cs)

	resp, err := cli.Requester().Do(context.Background(), &client.RequestOptions{
		Type:   client.RawRequest,
		Method: "GET",
		Path:   "/",
	})
	c.Assert(err, tc.ErrorIsNil)
	var v string
	err = resp.DecodeResult(&v)
	c.Assert(err, tc.NotNil)
	c.Check(cs.req.Header.Get("User-Agent"), tc.Equals, "some-agent/9.87")
}

func (cs *clientSuite) TestContentType(c *tc.C) {
	cli, err := client.New(&client.Config{})
	c.Assert(err, tc.ErrorIsNil)
	cli.SetDoer(cs)

	resp, err := cli.Requester().Do(context.Background(), &client.RequestOptions{
		Type:   client.RawRequest,
		Method: "GET",
		Path:   "/",
	})
	c.Assert(err, tc.ErrorIsNil)
	var v string
	err = resp.DecodeResult(&v)
	c.Assert(err, tc.NotNil)
	c.Check(cs.req.Header.Get("Content-Type"), tc.Equals, "application/json")
}

func (cs *clientSuite) TestClientJSONError(c *tc.C) {
	cs.rsp = `some non-json error message`
	_, err := cs.cli.SysInfo()
	c.Assert(err, tc.ErrorMatches, `cannot obtain system details: cannot decode "some non-json error message": invalid char.*`)
}

func (cs *clientSuite) TestDebugPost(c *tc.C) {
	cs.rsp = `{"type": "sync", "result":["res1","res2"]}`

	var result []string
	err := cs.cli.DebugPost("do-something", []string{"param1", "param2"}, &result)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(result, tc.DeepEquals, []string{"res1", "res2"})
	c.Check(cs.reqs, tc.HasLen, 1)
	c.Check(cs.reqs[0].Method, tc.Equals, "POST")
	c.Check(cs.reqs[0].URL.Path, tc.Equals, "/v1/debug")
	data, err := io.ReadAll(cs.reqs[0].Body)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(string(data), tc.DeepEquals, `{"action":"do-something","params":["param1","param2"]}`)
}

func (cs *clientSuite) TestDebugGet(c *tc.C) {
	cs.rsp = `{"type": "sync", "result":["res1","res2"]}`

	var result []string
	err := cs.cli.DebugGet("do-something", &result, map[string]string{"foo": "bar"})
	c.Assert(err, tc.ErrorIsNil)
	c.Check(result, tc.DeepEquals, []string{"res1", "res2"})
	c.Check(cs.reqs, tc.HasLen, 1)
	c.Check(cs.reqs[0].Method, tc.Equals, "GET")
	c.Check(cs.reqs[0].URL.Path, tc.Equals, "/v1/debug")
	c.Check(cs.reqs[0].URL.Query(), tc.DeepEquals, url.Values{"action": []string{"do-something"}, "foo": []string{"bar"}})
}

func (cs *clientSuite) TestNonExistentSocketErrors(c *tc.C) {
	cli, err := client.New(&client.Config{Socket: "/tmp/not-the-droids-you-are-looking-for"})
	c.Assert(err, tc.ErrorIsNil)

	_, err = cli.SysInfo()
	c.Check(err, tc.NotNil)
	var notFoundErr *client.SocketNotFoundError
	c.Check(errors.As(err, &notFoundErr), tc.Equals, true)

	c.Check(notFoundErr.Path, tc.Equals, "/tmp/not-the-droids-you-are-looking-for")
	c.Check(notFoundErr.Err, tc.NotNil)
}

func (cs *clientSuite) TestLatestWarningTime(c *tc.C) {
	cs.rsp = `{
		"result": {
			"version": "1.15.0",
			"boot-id": "BOOTID"
		},
		"status": "OK",
		"status-code": 200,
		"type": "sync",
		"latest-warning": "2018-09-19T12:44:19.680362867Z"
	}`

	info, err := cs.cli.SysInfo()
	c.Assert(err, tc.ErrorIsNil)
	c.Check(info, tc.DeepEquals, &client.SysInfo{
		Version: "1.15.0",
		BootID:  "BOOTID",
	})
	c.Check(cs.req.Method, tc.Equals, "GET")
	c.Check(cs.req.URL.Path, tc.Equals, "/v1/system-info")

	// this could be done at the end of any sync method
	latest := cs.cli.LatestWarningTime()
	c.Check(latest, tc.Equals, time.Date(2018, 9, 19, 12, 44, 19, 680362867, time.UTC))
}

func (cs *clientSuite) TestClientIntegrationUnixSocket(c *tc.C) {
	testUsername := "foo"
	testPassword := "bar"
	listener, err := net.Listen("unix", cs.socketPath)
	if err != nil {
		c.Fatalf("unable to listen on %q: %v", cs.socketPath, err)
	}
	defer listener.Close()

	handler := func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.URL.Path, tc.Equals, "/v1/system-info")
		c.Check(r.URL.RawQuery, tc.Equals, "")
		// tc.Basic Auth
		u, p, ok := r.BasicAuth()
		c.Check(ok, tc.Equals, true)
		c.Check(u, tc.Equals, testUsername)
		c.Check(p, tc.Equals, testPassword)

		fmt.Fprintln(w, `{"type":"sync", "result":{"version":"1"}}`)
	}

	srv := &httptest.Server{
		Listener: listener,
		Config:   &http.Server{Handler: http.HandlerFunc(handler)},
	}
	srv.Start()
	defer srv.Close()

	cli, err := client.New(&client.Config{
		Socket:        cs.socketPath,
		BasicUsername: testUsername,
		BasicPassword: testPassword,
	})
	c.Assert(err, tc.ErrorIsNil)
	si, err := cli.SysInfo()
	c.Assert(err, tc.ErrorIsNil)
	c.Check(si.Version, tc.Equals, "1")
}

func (cs *clientSuite) TestClientIntegrationHTTP(c *tc.C) {
	testUsername := "foo"
	testPassword := "bar"
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		c.Assert(err, tc.ErrorIsNil)
	}
	defer listener.Close()
	// Get the allocated port.
	testPort := listener.Addr().(*net.TCPAddr).Port

	handler := func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.URL.Path, tc.Equals, "/v1/system-info")
		c.Check(r.URL.RawQuery, tc.Equals, "")
		// tc.Basic Auth
		u, p, ok := r.BasicAuth()
		c.Check(ok, tc.Equals, true)
		c.Check(u, tc.Equals, testUsername)
		c.Check(p, tc.Equals, testPassword)

		fmt.Fprintln(w, `{"type":"sync", "result":{"version":"1"}}`)
	}

	srv := &httptest.Server{
		Listener: listener,
		Config:   &http.Server{Handler: http.HandlerFunc(handler)},
	}
	srv.Start()
	defer srv.Close()

	cli, err := client.New(&client.Config{
		BaseURL:       fmt.Sprintf("http://localhost:%d", testPort),
		BasicUsername: testUsername,
		BasicPassword: testPassword,
	})
	c.Assert(err, tc.ErrorIsNil)
	si, err := cli.SysInfo()
	c.Assert(err, tc.ErrorIsNil)
	c.Check(si.Version, tc.Equals, "1")
}

func (cs *clientSuite) TestClientIntegrationHTTPS(c *tc.C) {
	clientTLSCerts := createTestClientTLSCerts(c)
	serverTLSCerts, serverIDCert, serverFingerprint := createTestServerTLSCerts(c)

	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		c.Assert(err, tc.ErrorIsNil)
	}
	defer listener.Close()
	// Get the allocated port.
	testPort := listener.Addr().(*net.TCPAddr).Port

	handler := func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.URL.Path, tc.Equals, "/v1/system-info")
		c.Check(r.URL.RawQuery, tc.Equals, "")

		// Validate client identity cert.
		roots := x509.NewCertPool()
		roots.AddCert(clientTLSCerts.Leaf)
		opts := x509.VerifyOptions{
			Roots:     roots,
			KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		}
		incomingTLS := r.TLS.PeerCertificates[0]
		_, err := incomingTLS.Verify(opts)
		c.Assert(err, tc.ErrorIsNil)

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
	c.Assert(err, tc.ErrorIsNil)
	_, err = cli.SysInfo()
	c.Assert(err, tc.ErrorMatches, ".*cannot verify server: see TLS config options")

	// 2. Client with TLSServerInsecure true should allow a HTTPS connection with the server.
	cli, err = client.New(&client.Config{
		BaseURL:           fmt.Sprintf("https://localhost:%d", testPort),
		TLSServerInsecure: true,
		TLSClientIDCert:   clientTLSCerts,
	})
	c.Assert(err, tc.ErrorIsNil)

	cert, si, err := cli.SysInfoWithServerID()
	c.Assert(err, tc.ErrorIsNil)
	c.Check(si.Version, tc.Equals, "1")
	c.Check(cert, tc.DeepEquals, serverIDCert)

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
	c.Assert(err, tc.ErrorIsNil)

	cert, si, err = cli.SysInfoWithServerID()
	c.Assert(err, tc.ErrorIsNil)
	c.Check(si.Version, tc.Equals, "1")
	c.Check(cert, tc.DeepEquals, serverIDCert)

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
	c.Assert(err, tc.ErrorIsNil)

	cert, si, err = cli.SysInfoWithServerID()
	c.Assert(err, tc.ErrorIsNil)
	c.Check(si.Version, tc.Equals, "1")
	c.Check(cert, tc.DeepEquals, serverIDCert)
}

func createTestServerTLSCerts(c *tc.C) (*tls.Certificate, *x509.Certificate, string) {
	_, caKey, err := ed25519.GenerateKey(rand.Reader)
	c.Assert(err, tc.ErrorIsNil)

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
	c.Assert(err, tc.ErrorIsNil)

	caCert, err := x509.ParseCertificate(certDER)
	c.Assert(err, tc.ErrorIsNil)

	_, tlsKey, err := ed25519.GenerateKey(rand.Reader)
	c.Assert(err, tc.ErrorIsNil)

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
	c.Assert(err, tc.ErrorIsNil)

	tlsCert, err := x509.ParseCertificate(certDER)
	c.Assert(err, tc.ErrorIsNil)

	// Fingerprint
	fingerprint, err := client.GetIdentityFingerprint(caCert)
	c.Assert(err, tc.ErrorIsNil)

	tls := &tls.Certificate{
		Certificate: [][]byte{tlsCert.Raw, caCert.Raw},
		PrivateKey:  tlsKey,
		Leaf:        tlsCert,
	}
	return tls, caCert, fingerprint
}

func createTestClientTLSCerts(c *tc.C) *tls.Certificate {
	_, tlsKeyPair, err := ed25519.GenerateKey(rand.Reader)
	c.Assert(err, tc.ErrorIsNil)

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}

	// Self-signed certificate.
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, tlsKeyPair.Public(), tlsKeyPair)
	c.Assert(err, tc.ErrorIsNil)

	cert, err := x509.ParseCertificate(certDER)
	c.Assert(err, tc.ErrorIsNil)

	return &tls.Certificate{
		Certificate: [][]byte{cert.Raw},
		PrivateKey:  tlsKeyPair,
		Leaf:        cert,
	}
}
