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

	"github.com/canonical/tc"
)

func (cs *clientSuite) TestPair(c *tc.C) {
	idCert := &x509.Certificate{}
	cs.FakeTLSServer(idCert)
	cs.rsp = `{"type": "sync", "result": null}`
	cert, err := cs.cli.Pair()
	c.Assert(cert, tc.Equals, idCert)
	c.Assert(err, tc.IsNil)
	c.Assert(cs.req.Method, tc.Equals, "POST")
	c.Assert(cs.req.URL.Path, tc.Equals, "/v1/pairing")
	c.Assert(cs.req.URL.Query(), tc.DeepEquals, url.Values{})

	body, err := io.ReadAll(cs.req.Body)
	c.Assert(err, tc.IsNil)
	var m map[string]any
	err = json.Unmarshal(body, &m)
	c.Assert(err, tc.IsNil)
	c.Assert(m, tc.DeepEquals, map[string]any{
		"action": "pair",
	})
}

func (cs *clientSuite) TestPairError(c *tc.C) {
	idCert := &x509.Certificate{}
	cs.FakeTLSServer(idCert)
	cs.rsp = `{"type": "error", "result": {"message": "cannot pair client: pairing window not enabled"}}`
	cert, err := cs.cli.Pair()
	c.Assert(cert, tc.IsNil)
	c.Assert(err, tc.ErrorMatches, `cannot pair client: pairing window not enabled`)
	c.Assert(cs.req.Method, tc.Equals, "POST")
	c.Assert(cs.req.URL.Path, tc.Equals, "/v1/pairing")
}
