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

package client_test

import (
	"crypto/x509"
	"encoding/json"
	"io"
	"net/url"

	. "gopkg.in/check.v1"
)

func (cs *clientSuite) TestPair(c *C) {
	idCert := &x509.Certificate{}
	cs.FakeTLSServer(idCert)
	cs.rsp = `{"type": "sync", "result": null}`
	cert, err := cs.cli.Pair()
	c.Assert(cert, Equals, idCert)
	c.Assert(err, IsNil)
	c.Assert(cs.req.Method, Equals, "POST")
	c.Assert(cs.req.URL.Path, Equals, "/v1/pairing")
	c.Assert(cs.req.URL.Query(), DeepEquals, url.Values{})

	body, err := io.ReadAll(cs.req.Body)
	c.Assert(err, IsNil)
	var m map[string]any
	err = json.Unmarshal(body, &m)
	c.Assert(err, IsNil)
	c.Assert(m, DeepEquals, map[string]any{
		"action": "pair",
	})
}

func (cs *clientSuite) TestPairError(c *C) {
	idCert := &x509.Certificate{}
	cs.FakeTLSServer(idCert)
	cs.rsp = `{"type": "error", "result": {"message": "cannot pair client: pairing window not enabled"}}`
	cert, err := cs.cli.Pair()
	c.Assert(cert, IsNil)
	c.Assert(err, ErrorMatches, `cannot pair client: pairing window not enabled`)
	c.Assert(cs.req.Method, Equals, "POST")
	c.Assert(cs.req.URL.Path, Equals, "/v1/pairing")
}
