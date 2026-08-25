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

package opentelemetry_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	. "gopkg.in/check.v1"

	commonpb "github.com/canonical/pebble/internals/otlp/common/v1"
	logspb "github.com/canonical/pebble/internals/otlp/logs/v1"
	resourcepb "github.com/canonical/pebble/internals/otlp/resource/v1"
	"github.com/canonical/pebble/internals/overlord/logstate/opentelemetry"
	"github.com/canonical/pebble/internals/servicelog"
	"github.com/canonical/pebble/internals/testutil"
)

type suite struct{}

var _ = Suite(&suite{})

func Test(t *testing.T) {
	testutil.PrintGoroutineLeaks(t, TestingT)
}

func stringValue(s string) *commonpb.AnyValue {
	return &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: s}}
}

func attr(key, value string) *commonpb.KeyValue {
	return &commonpb.KeyValue{
		Key:   key,
		Value: stringValue(value),
	}
}

func logRecord(unixNano int64, message string) *logspb.LogRecord {
	return &logspb.LogRecord{
		TimeUnixNano: uint64(unixNano),
		Body:         stringValue(message),
	}
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

	expected := &logspb.LogsData{
		ResourceLogs: []*logspb.ResourceLogs{
			{
				Resource: &resourcepb.Resource{},
				ScopeLogs: []*logspb.ScopeLogs{{
					Scope: &commonpb.InstrumentationScope{Name: "pebble"},
					LogRecords: []*logspb.LogRecord{
						logRecord(1704026090000000000, "log line #1"),
						logRecord(1704026092000000000, "log line #3"),
						logRecord(1704026094000000000, "log line #5"),
						logRecord(1704026097000000000, "log line #8"),
					},
				}},
			},
			{
				Resource: &resourcepb.Resource{},
				ScopeLogs: []*logspb.ScopeLogs{{
					Scope: &commonpb.InstrumentationScope{Name: "pebble"},
					LogRecords: []*logspb.LogRecord{
						logRecord(1704026091000000000, "log line #2"),
						logRecord(1704026096000000000, "log line #7"),
					},
				}},
			},
			{
				Resource: &resourcepb.Resource{},
				ScopeLogs: []*logspb.ScopeLogs{{
					Scope: &commonpb.InstrumentationScope{Name: "pebble"},
					LogRecords: []*logspb.LogRecord{
						logRecord(1704026093000000000, "log line #4"),
						logRecord(1704026095000000000, "log line #6"),
					},
				}},
			},
			{
				Resource: &resourcepb.Resource{},
				ScopeLogs: []*logspb.ScopeLogs{{
					Scope: &commonpb.InstrumentationScope{Name: "pebble"},
					LogRecords: []*logspb.LogRecord{
						logRecord(1704026098000000000, "log line #9"),
					},
				}},
			},
		},
	}

	numRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Assert(r.Method, Equals, http.MethodPost)
		c.Assert(r.Header.Get("Content-Type"), Equals, "application/x-protobuf")
		c.Assert(r.Header.Get("User-Agent"), Equals, "pebble/1.23.0")
		numRequests++
		reqBody, err := io.ReadAll(r.Body)
		c.Assert(err, IsNil)
		var data logspb.LogsData
		err = proto.Unmarshal(reqBody, &data)
		c.Assert(err, IsNil)
		c.Assert(proto.Equal(&data, expected), Equals, true, Commentf("got: %v\nwant: %v", &data, expected))
	}))
	defer server.Close()

	client := opentelemetry.NewClient(&opentelemetry.ClientOptions{
		Location:  server.URL,
		UserAgent: "pebble/1.23.0",
		ScopeName: "pebble",
	})
	for _, entry := range input {
		err := client.Add(entry)
		c.Assert(err, IsNil)
	}

	err := client.Flush(context.Background())
	c.Assert(err, IsNil)
	c.Assert(numRequests, Equals, 1)
}

func (*suite) TestCustomHeaders(c *C) {
	var gotAuth, gotCustom string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotCustom = r.Header.Get("X-Custom-Header")
	}))
	defer server.Close()

	client := opentelemetry.NewClient(&opentelemetry.ClientOptions{
		Location: server.URL,
		Headers: map[string]string{
			"Authorization":   "Bearer sometoken",
			"X-Custom-Header": "custom-value",
		},
	})
	err := client.Add(servicelog.Entry{Message: "hello"})
	c.Assert(err, IsNil)

	err = client.Flush(context.Background())
	c.Assert(err, IsNil)
	c.Assert(gotAuth, Equals, "Bearer sometoken")
	c.Assert(gotCustom, Equals, "custom-value")
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

	client := opentelemetry.NewClient(&opentelemetry.ClientOptions{Location: server.URL})
	err := client.Add(servicelog.Entry{
		Time:    time.Now(),
		Service: "svc1",
		Message: "this is a log line\n",
	})
	c.Assert(err, IsNil)

	flushReturned := make(chan struct{})
	go func() {
		// Cancel the Flush context quickly
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()

		err := client.Flush(ctx)
		c.Check(err, ErrorMatches, ".*context deadline exceeded.*")
		close(flushReturned)
	}()

	// Check Flush returns quickly after context timeout
	select {
	case <-flushReturned:
	case <-time.After(1 * time.Second):
		c.Fatal("opentelemetryClient.Flush took too long to return after context timeout")
	}
}

func (*suite) TestServerTimeout(c *C) {
	stopRequest := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-stopRequest
	}))
	defer server.Close()
	defer close(stopRequest)

	client := opentelemetry.NewClient(&opentelemetry.ClientOptions{
		Location:       server.URL,
		RequestTimeout: 1 * time.Microsecond,
	})
	err := client.Add(servicelog.Entry{
		Time:    time.Now(),
		Service: "svc1",
		Message: "this is a log line\n",
	})
	c.Assert(err, IsNil)

	err = client.Flush(context.Background())
	c.Assert(err, ErrorMatches, ".*context deadline exceeded.*")
}

func (*suite) TestBufferFull(c *C) {
	client := opentelemetry.NewClient(&opentelemetry.ClientOptions{
		TargetName:        "tgt1",
		Location:          "fake",
		MaxRequestEntries: 3,
	})

	addEntry := func(s string) {
		err := client.Add(servicelog.Entry{Message: s})
		c.Assert(err, IsNil)
	}

	// Check that the client's buffer is as expected
	buffer := opentelemetry.GetBuffer(client)
	checkBuffer := func(expected []any) {
		if len(buffer) != len(expected) {
			c.Fatalf("buffer length is %v, expected %v", len(buffer), len(expected))
		}

		for i := range expected {
			// 'nil' means c.buffer[i] should be zero
			if expected[i] == nil {
				c.Assert(buffer[i], DeepEquals, opentelemetry.EntryWithService{},
					Commentf("buffer[%d] should be zero, obtained %v", i, buffer[i]))
				continue
			}

			// Otherwise, check buffer message matches string
			msg := expected[i].(string)
			c.Assert(opentelemetry.GetMessage(buffer[i]), Equals, msg)
		}
	}

	checkBuffer([]any{nil, nil, nil, nil, nil, nil})
	addEntry("1")
	checkBuffer([]any{"1", nil, nil, nil, nil, nil})
	addEntry("2")
	checkBuffer([]any{"1", "2", nil, nil, nil, nil})
	addEntry("3")
	checkBuffer([]any{"1", "2", "3", nil, nil, nil})
	addEntry("4")
	checkBuffer([]any{nil, "2", "3", "4", nil, nil})
	addEntry("5")
	checkBuffer([]any{nil, nil, "3", "4", "5", nil})
	addEntry("6")
	checkBuffer([]any{nil, nil, nil, "4", "5", "6"})
	addEntry("7")
	checkBuffer([]any{"5", "6", "7", nil, nil, nil})
}

func (*suite) TestLabels(c *C) {
	var expected *logspb.LogsData

	received := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqBody, err := io.ReadAll(r.Body)
		c.Assert(err, IsNil)
		var data logspb.LogsData
		err = proto.Unmarshal(reqBody, &data)
		c.Assert(err, IsNil)
		c.Assert(proto.Equal(&data, expected), Equals, true, Commentf("got: %v\nwant: %v", &data, expected))
		close(received)
	}))
	defer server.Close()

	client := opentelemetry.NewClient(&opentelemetry.ClientOptions{
		Location:  server.URL,
		ScopeName: "pebble",
	})

	client.SetLabels("svc1", map[string]string{
		"label1": "val1",
		"label2": "val2",
	})

	err := client.Add(servicelog.Entry{
		Service: "svc1",
		Time:    time.Date(2023, 10, 3, 4, 20, 33, 0, time.UTC),
		Message: "hello",
	})
	c.Assert(err, IsNil)

	expected = &logspb.LogsData{
		ResourceLogs: []*logspb.ResourceLogs{{
			Resource: &resourcepb.Resource{
				Attributes: []*commonpb.KeyValue{
					attr("service.name", "svc1"),
					attr("label1", "val1"),
					attr("label2", "val2"),
				},
			},
			ScopeLogs: []*logspb.ScopeLogs{{
				Scope: &commonpb.InstrumentationScope{Name: "pebble"},
				LogRecords: []*logspb.LogRecord{
					logRecord(1696306833000000000, "hello"),
				},
			}},
		}},
	}

	err = client.Flush(context.Background())
	c.Assert(err, IsNil)
	select {
	case <-received:
	case <-time.After(1 * time.Second):
		c.Fatal("timed out waiting for request")
	}
}
