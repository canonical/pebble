// Copyright (c) 2023 Canonical Ltd
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

package logstate

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/canonical/pebble/internal/servicelog"
	. "gopkg.in/check.v1"
)

type lokiSuite struct{}

var _ = Suite(&lokiSuite{})

func (s *lokiSuite) TestLokiClient(c *C) {
	entries := []servicelog.Entry{{
		Time:    time.Date(2021, 5, 26, 12, 37, 1, 0, time.UTC),
		Service: "foo",
		Message: "this is a log entry",
	}, {
		Time:    time.Date(2021, 5, 26, 12, 37, 3, 0, time.UTC),
		Service: "foo",
		Message: "this is a later log entry",
	}, {
		Time:    time.Date(2021, 5, 26, 12, 37, 6, 0, time.UTC),
		Service: "foo",
		Message: "this is an even later log entry",
	}}

	expected := `{"streams":[{"stream":{"pebble_service":"foo"},"values":[["1622032621000000000","this is a log entry"],["1622032623000000000","this is a later log entry"],["1622032626000000000","this is an even later log entry"]]}]}`

	requests := make(chan string, 1)
	srv := newFakeLokiServer(requests)
	defer srv.Close()

	cl := newLokiClient(srv.URL())
	err := cl.Send(entries)
	c.Assert(err, IsNil)

	select {
	case msg := <-requests:
		c.Check(msg, Equals, expected)
	default:
		c.Fatalf("no request received; expected:\n%s", expected)
	}

}

type fakeLokiServer struct {
	srv      *httptest.Server
	requests chan string
}

func newFakeLokiServer(requests chan string) *fakeLokiServer {
	s := &fakeLokiServer{
		requests: requests,
	}

	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := ioutil.ReadAll(r.Body)
		if err != nil {
			http.Error(w, fmt.Sprintf("error reading request body: %v", err),
				http.StatusInternalServerError)
			return
		}

		requests <- string(data)
	}))

	return s
}

func (s *fakeLokiServer) URL() string {
	return s.srv.URL
}

func (s *fakeLokiServer) Close() {
	s.srv.Close()
}
