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
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/canonical/tc"
)

type responseSuite struct{}

func TestResponseSuite(t *testing.T) {
	tc.Run(t, &responseSuite{})
}

func (s *responseSuite) TestRespSetsLocationIfAccepted(c *tc.C) {
	rec := httptest.NewRecorder()

	rsp := &resp{
		Status: http.StatusAccepted,
		Result: map[string]any{
			"resource": "foo/bar",
		},
	}

	rsp.ServeHTTP(rec, nil)
	hdr := rec.Result().Header
	c.Check(hdr.Get("Location"), tc.Equals, "foo/bar")
}

func (s *responseSuite) TestRespSetsLocationIfCreated(c *tc.C) {
	rec := httptest.NewRecorder()

	rsp := &resp{
		Status: http.StatusCreated,
		Result: map[string]any{
			"resource": "foo/bar",
		},
	}

	rsp.ServeHTTP(rec, nil)
	hdr := rec.Result().Header
	c.Check(hdr.Get("Location"), tc.Equals, "foo/bar")
}

func (s *responseSuite) TestRespDoesNotSetLocationIfOther(c *tc.C) {
	rec := httptest.NewRecorder()

	rsp := &resp{
		Status: http.StatusTeapot, // I'm a teapot
		Result: map[string]any{
			"resource": "foo/bar",
		},
	}

	rsp.ServeHTTP(rec, nil)
	hdr := rec.Result().Header
	c.Check(hdr.Get("Location"), tc.Equals, "")
}

func (s *responseSuite) TestFileResponseSetsContentDisposition(c *tc.C) {
	const filename = "icon.png"

	path := filepath.Join(c.MkDir(), filename)
	err := os.WriteFile(path, nil, os.ModePerm)
	c.Assert(err, tc.ErrorIsNil)

	rec := httptest.NewRecorder()
	rsp := fileResponse(path)
	req, err := http.NewRequest("GET", "", nil)
	c.Assert(err, tc.ErrorIsNil)

	rsp.ServeHTTP(rec, req)

	hdr := rec.Result().Header
	c.Check(hdr.Get("Content-Disposition"), tc.Equals,
		fmt.Sprintf("attachment; filename=%s", filename))
}

// This diverges from snapd. For historical reasons snapd must send a null result
// in this case, but there are no old clients to be worried about here.
func (s *responseSuite) TestRespJSONWithNullResult(c *tc.C) {
	rj := &respJSON{Result: nil}
	data, err := json.Marshal(rj)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(string(data), tc.Equals, `{"type":"","status-code":0}`)
}

func (s *responseSuite) TestErrorResponderPrintfsWithArgs(c *tc.C) {
	teapot := makeErrorResponder(http.StatusTeapot)

	rec := httptest.NewRecorder()
	rsp := teapot("system memory below %d%%.", 1)
	req, err := http.NewRequest("GET", "", nil)
	c.Assert(err, tc.ErrorIsNil)
	rsp.ServeHTTP(rec, req)

	var v struct{ Result errorResult }
	c.Assert(json.NewDecoder(rec.Body).Decode(&v), tc.IsNil)

	c.Check(v.Result.Message, tc.Equals, "system memory below 1%.")
}

func (s *responseSuite) TestErrorResponderDoesNotPrintfAlways(c *tc.C) {
	teapot := makeErrorResponder(http.StatusTeapot)

	rec := httptest.NewRecorder()
	rsp := teapot("system memory below 1%.")
	req, err := http.NewRequest("GET", "", nil)
	c.Assert(err, tc.ErrorIsNil)
	rsp.ServeHTTP(rec, req)

	var v struct{ Result errorResult }
	c.Assert(json.NewDecoder(rec.Body).Decode(&v), tc.IsNil)

	c.Check(v.Result.Message, tc.Equals, "system memory below 1%.")
}
