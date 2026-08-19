// Copyright (c) 2026 Canonical Ltd
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
	resourcepb "github.com/canonical/pebble/internals/otlp/resource/v1"
	tracepb "github.com/canonical/pebble/internals/otlp/trace/v1"
	"github.com/canonical/pebble/internals/overlord/tracestate/opentelemetry"
	"github.com/canonical/pebble/internals/testutil"
)

type suite struct{}

var _ = Suite(&suite{})

func Test(t *testing.T) {
	testutil.PrintGoroutineLeaks(t, TestingT)
}

func attr(key, value string) *commonpb.KeyValue {
	return &commonpb.KeyValue{
		Key:   key,
		Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: value}},
	}
}

func attrMap(attrs []*commonpb.KeyValue) map[string]string {
	m := make(map[string]string, len(attrs))
	for _, a := range attrs {
		m[a.GetKey()] = a.GetValue().GetStringValue()
	}
	return m
}

func (*suite) TestFlushEnrichesAndSends(c *C) {
	input := []*tracepb.ResourceSpans{{
		Resource: &resourcepb.Resource{
			Attributes: []*commonpb.KeyValue{
				attr("service.name", "sdk-supplied-name"),
				attr("existing.attr", "keep-me"),
			},
		},
		ScopeSpans: []*tracepb.ScopeSpans{{
			Spans: []*tracepb.Span{{Name: "handle-request"}},
		}},
	}}

	received := make(chan *tracepb.TracesData, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Assert(r.Method, Equals, http.MethodPost)
		c.Assert(r.URL.Path, Equals, "/v1/traces")
		c.Assert(r.Header.Get("Content-Type"), Equals, "application/x-protobuf")
		c.Assert(r.Header.Get("User-Agent"), Equals, "pebble/1.23.0")

		body, err := io.ReadAll(r.Body)
		c.Assert(err, IsNil)
		var data tracepb.TracesData
		err = proto.Unmarshal(body, &data)
		c.Assert(err, IsNil)
		received <- &data
	}))
	defer server.Close()

	client := opentelemetry.NewClient(&opentelemetry.ClientOptions{
		Location:  server.URL,
		UserAgent: "pebble/1.23.0",
	})

	err := client.Add("myservice", "instance-123", map[string]string{"env": "prod"}, input)
	c.Assert(err, IsNil)

	err = client.Flush(context.Background())
	c.Assert(err, IsNil)

	select {
	case data := <-received:
		c.Assert(data.GetResourceSpans(), HasLen, 1)
		rs := data.GetResourceSpans()[0]
		attrs := attrMap(rs.GetResource().GetAttributes())
		c.Check(attrs["service.name"], Equals, "myservice") // overridden
		c.Check(attrs["pebble.service"], Equals, "myservice")
		c.Check(attrs["service.instance.id"], Equals, "instance-123")
		c.Check(attrs["existing.attr"], Equals, "keep-me")
		c.Check(attrs["env"], Equals, "prod")
		c.Assert(rs.GetScopeSpans(), HasLen, 1)
		c.Check(rs.GetScopeSpans()[0].GetSpans()[0].GetName(), Equals, "handle-request")
	case <-time.After(1 * time.Second):
		c.Fatal("timed out waiting for request")
	}
}

func (*suite) TestServiceInstanceIDNotOverwritten(c *C) {
	input := []*tracepb.ResourceSpans{{
		Resource: &resourcepb.Resource{
			Attributes: []*commonpb.KeyValue{
				attr("service.instance.id", "sdk-supplied-id"),
			},
		},
	}}

	received := make(chan *tracepb.TracesData, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		c.Assert(err, IsNil)
		var data tracepb.TracesData
		err = proto.Unmarshal(body, &data)
		c.Assert(err, IsNil)
		received <- &data
	}))
	defer server.Close()

	client := opentelemetry.NewClient(&opentelemetry.ClientOptions{Location: server.URL})
	err := client.Add("svc1", "pebble-generated-id", nil, input)
	c.Assert(err, IsNil)
	err = client.Flush(context.Background())
	c.Assert(err, IsNil)

	select {
	case data := <-received:
		attrs := attrMap(data.GetResourceSpans()[0].GetResource().GetAttributes())
		c.Check(attrs["service.instance.id"], Equals, "sdk-supplied-id")
	case <-time.After(1 * time.Second):
		c.Fatal("timed out waiting for request")
	}
}

func (*suite) TestFlushEmptyIsNoOp(c *C) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
	}))
	defer server.Close()

	client := opentelemetry.NewClient(&opentelemetry.ClientOptions{Location: server.URL})
	err := client.Flush(context.Background())
	c.Assert(err, IsNil)
	c.Check(requests, Equals, 0)
}

func (*suite) TestFlushCancelContext(c *C) {
	serverCtx, killServer := context.WithCancel(context.Background())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-serverCtx.Done():
		case <-time.After(10 * time.Second):
		}
	}))
	defer server.Close()
	defer killServer()

	client := opentelemetry.NewClient(&opentelemetry.ClientOptions{Location: server.URL})
	err := client.Add("svc1", "", nil, []*tracepb.ResourceSpans{{}})
	c.Assert(err, IsNil)

	flushReturned := make(chan struct{})
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()

		err := client.Flush(ctx)
		c.Check(err, ErrorMatches, ".*context deadline exceeded.*")
		close(flushReturned)
	}()

	select {
	case <-flushReturned:
	case <-time.After(1 * time.Second):
		c.Fatal("Client.Flush took too long to return after context timeout")
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
	err := client.Add("svc1", "", nil, []*tracepb.ResourceSpans{{}})
	c.Assert(err, IsNil)

	err = client.Flush(context.Background())
	c.Assert(err, ErrorMatches, ".*context deadline exceeded.*")
}

func (*suite) TestDropsBatchesOn4xx(c *C) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	client := opentelemetry.NewClient(&opentelemetry.ClientOptions{Location: server.URL})
	err := client.Add("svc1", "", nil, []*tracepb.ResourceSpans{{}})
	c.Assert(err, IsNil)

	err = client.Flush(context.Background())
	c.Assert(err, ErrorMatches, ".*400.*")

	// Batch should have been dropped, so a second flush is a no-op (no
	// further requests).
	requests := 0
	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
	})
	err = client.Flush(context.Background())
	c.Assert(err, IsNil)
	c.Check(requests, Equals, 0)
}

func (*suite) TestRetainsBatchesOn5xx(c *C) {
	failing := true
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if failing {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := opentelemetry.NewClient(&opentelemetry.ClientOptions{Location: server.URL})
	err := client.Add("svc1", "", nil, []*tracepb.ResourceSpans{{}})
	c.Assert(err, IsNil)

	err = client.Flush(context.Background())
	c.Assert(err, ErrorMatches, ".*503.*")
	c.Check(requests, Equals, 1)

	// Batch should have been retained - flushing again should retry it.
	failing = false
	err = client.Flush(context.Background())
	c.Assert(err, IsNil)
	c.Check(requests, Equals, 2)
}

func (*suite) TestBufferFullDropsOldestBatch(c *C) {
	client := opentelemetry.NewClient(&opentelemetry.ClientOptions{
		TargetName:         "tgt1",
		Location:           "fake",
		MaxBufferedBatches: 2,
	})

	err := client.Add("svc1", "", nil, []*tracepb.ResourceSpans{{}})
	c.Assert(err, IsNil)
	err = client.Add("svc2", "", nil, []*tracepb.ResourceSpans{{}})
	c.Assert(err, IsNil)
	c.Assert(opentelemetry.GetBatches(client), HasLen, 2)

	err = client.Add("svc3", "", nil, []*tracepb.ResourceSpans{{}})
	c.Assert(err, IsNil)

	batches := opentelemetry.GetBatches(client)
	c.Assert(batches, HasLen, 2)
	c.Check(opentelemetry.BatchService(batches[0]), Equals, "svc2")
	c.Check(opentelemetry.BatchService(batches[1]), Equals, "svc3")
}
