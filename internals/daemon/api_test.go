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

package daemon

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/canonical/pebble/internals/overlord/pairingstate"
	"github.com/canonical/pebble/internals/overlord/restart"
	"github.com/canonical/pebble/internals/plan"
	"github.com/canonical/pebble/internals/reaper"
	"github.com/canonical/tc"
)

func TestApiSuite(t *testing.T) {
	tc.Run(t, &apiSuite{})
}

type apiSuite struct {
	d *Daemon

	pebbleDir string

	vars map[string]string

	restoreMuxVars  func()
	overlordStarted bool
}

func (s *apiSuite) SetUpTest(c *tc.C) {
	plan.RegisterSectionExtension(pairingstate.PairingField, &pairingstate.SectionExtension{})
	err := reaper.Start()
	if err != nil {
		c.Fatalf("cannot start reaper: %v", err)
	}

	s.restoreMuxVars = FakeMuxVars(s.muxVars)
	s.pebbleDir = c.MkDir()

	c.Cleanup(func() {
		s.d = nil
		s.pebbleDir = ""
		s.vars = nil
		s.restoreMuxVars = nil
		s.overlordStarted = false
	})
}

func (s *apiSuite) TearDownTest(c *tc.C) {
	if s.overlordStarted {
		s.d.Overlord().Stop()
		s.overlordStarted = false
	}
	s.d = nil
	s.pebbleDir = ""
	s.restoreMuxVars()

	err := reaper.Stop()
	if err != nil {
		c.Fatalf("cannot stop reaper: %v", err)
	}
	plan.UnregisterSectionExtension(pairingstate.PairingField)
}

func (s *apiSuite) muxVars(*http.Request) map[string]string {
	return s.vars
}

func (s *apiSuite) daemon(c *tc.C) *Daemon {
	if s.d != nil {
		panic("called daemon() twice")
	}
	d, err := New(&Options{Dir: s.pebbleDir})
	c.Assert(err, tc.ErrorIsNil)
	d.addRoutes()

	c.Assert(d.overlord.StartUp(), tc.IsNil)

	s.d = d
	return d
}

func (s *apiSuite) startOverlord() {
	s.overlordStarted = true
	s.d.overlord.Loop()
}

func apiCmd(path string) *Command {
	for _, cmd := range API {
		if cmd.Path == path {
			return cmd
		}
	}
	panic("no command with path " + path)
}

func (s *apiSuite) TestSysInfo(c *tc.C) {
	sysInfoCmd := apiCmd("/v1/system-info")
	c.Assert(sysInfoCmd.GET, tc.NotNil)
	c.Check(sysInfoCmd.PUT, tc.IsNil)
	c.Check(sysInfoCmd.POST, tc.IsNil)

	rec := httptest.NewRecorder()

	d := s.daemon(c)
	d.Version = "42b1"
	d.options.HTTPAddress = ":4000"
	d.options.HTTPSAddress = ":4443"
	state := d.overlord.State()
	state.Lock()
	_, err := restart.Manager(state, "ffffffff-ffff-ffff-ffff-ffffffffffff", nil)
	state.Unlock()
	c.Assert(err, tc.ErrorIsNil)

	sysInfoCmd.GET(sysInfoCmd, nil, nil).ServeHTTP(rec, nil)
	c.Check(rec.Code, tc.Equals, 200)
	c.Check(rec.Result().Header.Get("Content-Type"), tc.Equals, "application/json")

	expected := map[string]any{
		"boot-id":       "ffffffff-ffff-ffff-ffff-ffffffffffff",
		"http-address":  ":4000",
		"https-address": ":4443",
		"version":       "42b1",
	}
	var rsp resp
	c.Assert(json.Unmarshal(rec.Body.Bytes(), &rsp), tc.IsNil)
	c.Check(rsp.Status, tc.Equals, 200)
	c.Check(rsp.Type, tc.Equals, ResponseTypeSync)
	c.Check(rsp.Result, tc.DeepEquals, expected)
}

func fakeEnv(key, value string) (restore func()) {
	oldEnv, envWasSet := os.LookupEnv(key)
	err := os.Setenv(key, value)
	if err != nil {
		panic(err)
	}
	return func() {
		var err error
		if envWasSet {
			err = os.Setenv(key, oldEnv)
		} else {
			err = os.Unsetenv(key)
		}
		if err != nil {
			panic(err)
		}
	}
}

func createTestClientCertificate(c *tc.C) *x509.Certificate {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	c.Assert(err, tc.ErrorIsNil)

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, privateKey.Public(), privateKey)
	c.Assert(err, tc.ErrorIsNil)

	cert, err := x509.ParseCertificate(certDER)
	c.Assert(err, tc.ErrorIsNil)
	return cert
}
