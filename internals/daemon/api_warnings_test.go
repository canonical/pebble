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
	"bytes"
	"io"
	"net/http"
	"net/url"

	"gopkg.in/check.v1"
)

func (s *apiSuite) testWarnings(c *check.C, all bool, body io.Reader) interface{} {
	s.daemon(c)

	warningsCmd := apiCmd("/v1/warnings")

	method := "GET"
	f := warningsCmd.GET
	if body != nil {
		method = "POST"
		f = warningsCmd.POST
	}
	q := url.Values{}
	if all {
		q.Set("select", "all")
	}
	req, err := http.NewRequest(method, "/v1/warnings?"+q.Encode(), body)
	c.Assert(err, check.IsNil)

	rsp, ok := f(warningsCmd, req, nil).(*resp)
	c.Assert(ok, check.Equals, true)

	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Check(rsp.Status, check.Equals, 200)
	c.Assert(rsp.Result, check.NotNil)
	return rsp.Result
}

func (s *apiSuite) TestAllWarnings(c *check.C) {
	result := s.testWarnings(c, true, nil)
	c.Check(result, check.DeepEquals, []string{})
}

func (s *apiSuite) TestSomeWarnings(c *check.C) {
	result := s.testWarnings(c, false, nil)
	c.Check(result, check.DeepEquals, []string{})
}

func (s *apiSuite) TestAckWarnings(c *check.C) {
	result := s.testWarnings(c, false, bytes.NewReader([]byte(`{"action": "okay", "timestamp": "2006-01-02T15:04:05Z"}`)))
	c.Check(result, check.DeepEquals, 0)
}
