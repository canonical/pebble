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

package loki_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/overlord/logstate/loki"
	"github.com/canonical/pebble/internals/plan"
	"github.com/canonical/pebble/internals/servicelog"
)

type suite struct{}

var _ = Suite(&suite{})

func Test(t *testing.T) {
	TestingT(t)
}

func (*suite) TestRequest(c *C) {
	input := []servicelog.Entry{{
		Time:    time.Date(2023, 12, 31, 12, 34, 50, 0, time.UTC),
		Service: "svc1",
		Message: "log line #1\n",
	}, {
		Time:    time.Date(2023, 12, 31, 12, 34, 51, 0, time.UTC),
		Service: "svc2",
		Message: "log line #2\n",
	}, {
		Time:    time.Date(2023, 12, 31, 12, 34, 52, 0, time.UTC),
		Service: "svc1",
		Message: "log line #3\n",
	}, {
		Time:    time.Date(2023, 12, 31, 12, 34, 53, 0, time.UTC),
		Service: "svc3",
		Message: "log line #4\n",
	}, {
		Time:    time.Date(2023, 12, 31, 12, 34, 54, 0, time.UTC),
		Service: "svc1",
		Message: "log line #5\n",
	}, {
		Time:    time.Date(2023, 12, 31, 12, 34, 55, 0, time.UTC),
		Service: "svc3",
		Message: "log line #6\n",
	}, {
		Time:    time.Date(2023, 12, 31, 12, 34, 56, 0, time.UTC),
		Service: "svc2",
		Message: "log line #7\n",
	}, {
		Time:    time.Date(2023, 12, 31, 12, 34, 57, 0, time.UTC),
		Service: "svc1",
		Message: "log line #8\n",
	}, {
		Time:    time.Date(2023, 12, 31, 12, 34, 58, 0, time.UTC),
		Service: "svc4",
		Message: "log line #9\n",
	}}

	expected := `
{
  "streams": [
    {
      "stream": {
        "pebble_service": "svc1"
      },
      "values": [
          [ "1704026090000000000", "log line #1" ],
          [ "1704026092000000000", "log line #3" ],
          [ "1704026094000000000", "log line #5" ],
          [ "1704026097000000000", "log line #8" ]
      ]
    },
    {
      "stream": {
        "pebble_service": "svc2"
      },
      "values": [
          [ "1704026091000000000", "log line #2" ],
          [ "1704026096000000000", "log line #7" ]
      ]
    },
    {
      "stream": {
        "pebble_service": "svc3"
      },
      "values": [
          [ "1704026093000000000", "log line #4" ],
          [ "1704026095000000000", "log line #6" ]
      ]
    },
    {
      "stream": {
        "pebble_service": "svc4"
      },
      "values": [
          [ "1704026098000000000", "log line #9" ]
      ]
    }
  ]
}
`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Assert(r.Method, Equals, http.MethodPost)
		c.Assert(r.Header.Get("Content-Type"), Equals, "application/json; charset=utf-8")

		reqBody, err := io.ReadAll(r.Body)
		c.Assert(err, IsNil)
		expFlattened, err := flattenJSON(expected)
		c.Assert(err, IsNil)
		c.Assert(string(reqBody), Equals, expFlattened)
	}))
	defer server.Close()

	client := loki.NewClient(&plan.LogTarget{Location: server.URL})
	for _, entry := range input {
		err := client.Write(context.Background(), entry)
		c.Assert(err, IsNil)
	}

	err := client.Flush(context.Background())
	c.Assert(err, IsNil)
}

func (*suite) TestFlushCancelContext(c *C) {
	serverCtx, killServer := context.WithCancel(context.Background())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-serverCtx.Done():
		// Simulate a slow-responding server
		case <-time.After(10 * time.Second):
		}
	}))
	defer server.Close()
	defer killServer()

	client := loki.NewClient(&plan.LogTarget{Location: server.URL})
	err := client.Write(context.Background(), servicelog.Entry{
		Time:    time.Now(),
		Service: "svc1",
		Message: "this is a log line\n",
	})
	c.Assert(err, IsNil)

	flushReturned := make(chan struct{})
	go func() {
		// Cancel the Flush context quickly
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Microsecond)
		defer cancel()

		err := client.Flush(ctx)
		c.Assert(err, ErrorMatches, ".*context deadline exceeded.*")
		close(flushReturned)
	}()

	// Check Flush returns quickly after context timeout
	select {
	case <-flushReturned:
	case <-time.After(1 * time.Second):
		c.Fatal("lokiClient.Flush took too long to return after context timeout")
	}
}

func (*suite) TestServerTimeout(c *C) {
	restore := loki.FakeRequestTimeout(1 * time.Microsecond)
	defer restore()

	stopRequest := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-stopRequest
	}))
	defer server.Close()
	defer close(stopRequest)

	client := loki.NewClient(&plan.LogTarget{Location: server.URL})
	err := client.Write(context.Background(), servicelog.Entry{
		Time:    time.Now(),
		Service: "svc1",
		Message: "this is a log line\n",
	})
	c.Assert(err, IsNil)

	err = client.Flush(context.Background())
	c.Assert(err, ErrorMatches, ".*context deadline exceeded.*")
}

// Strips all extraneous whitespace from JSON
func flattenJSON(s string) (string, error) {
	var v any
	err := json.Unmarshal([]byte(s), &v)
	if err != nil {
		return "", err
	}

	b, err := json.Marshal(v)
	return string(b), err
}
