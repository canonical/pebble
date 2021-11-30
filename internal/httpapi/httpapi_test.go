// Copyright (c) 2021 Canonical Ltd
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

package httpapi_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internal/httpapi"
)

func Test(t *testing.T) { TestingT(t) }

type APISuite struct{}

var _ = Suite(&APISuite{})

func (s *APISuite) TestNotFound(c *C) {
	api := httpapi.NewAPI(nil)
	status, response := serve(c, api, "GET", "/foo", nil)

	c.Assert(status, Equals, 404)
	c.Assert(response, DeepEquals, map[string]interface{}{
		"error": "not found",
	})
}

func (s *APISuite) TestMethodNotAllowed(c *C) {
	api := httpapi.NewAPI(nil)
	status, response := serve(c, api, "POST", "/v1/health", nil)

	c.Assert(status, Equals, 405)
	c.Assert(response, DeepEquals, map[string]interface{}{
		"error": "method not allowed",
	})
}

func serve(c *C, api *httpapi.API, method, url string, body io.Reader) (int, map[string]interface{}) {
	recorder := httptest.NewRecorder()
	request, err := http.NewRequest(method, url, body)
	c.Assert(err, IsNil)

	api.ServeHTTP(recorder, request)

	c.Assert(recorder.Result().Header.Get("Content-Type"), Equals, "application/json; charset=utf-8")
	var response map[string]interface{}
	err = json.NewDecoder(recorder.Result().Body).Decode(&response)
	c.Assert(err, IsNil)
	return recorder.Result().StatusCode, response
}
