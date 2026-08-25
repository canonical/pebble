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

package daemon

import (
	"io"
	"mime"
	"net/http"
	"strconv"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	commonpb "github.com/canonical/pebble/internals/otlp/common/v1"
	logspb "github.com/canonical/pebble/internals/otlp/logs/v1"
	metricspb "github.com/canonical/pebble/internals/otlp/metrics/v1"
	tracepb "github.com/canonical/pebble/internals/otlp/trace/v1"
)

const (
	otlpContentTypeJSON     = "application/json"
	otlpContentTypeProtobuf = "application/x-protobuf"

	// maxOTLPBodyBytes bounds the size of a single OTLP export request that
	// Pebble's local receiver will accept.
	maxOTLPBodyBytes = 4 * 1024 * 1024
)

// v1PostOTLPLogs implements the local OTLP/HTTP logs receiver endpoint,
// POST /v1/services/{name}/otlp/v1/logs. It accepts a JSON or Protobuf encoded
// ExportLogsServiceRequest, and appends the log records to the named service's
// ring buffer.
func v1PostOTLPLogs(c *Command, r *http.Request, _ *UserState) Response {
	serviceName := muxVars(r)["name"]

	contentType := r.Header.Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = contentType
	}

	var isProtobuf bool
	switch mediaType {
	case otlpContentTypeJSON:
		isProtobuf = false
	case otlpContentTypeProtobuf:
		isProtobuf = true
	default:
		return UnsupportedMediaType("unsupported content type %q, must be %q or %q",
			contentType, otlpContentTypeJSON, otlpContentTypeProtobuf)
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxOTLPBodyBytes+1))
	if err != nil {
		return BadRequest("cannot read request body: %v", err)
	}
	if len(body) > maxOTLPBodyBytes {
		return BadRequest("request body exceeds maximum size of %d bytes", maxOTLPBodyBytes)
	}

	var data logspb.LogsData
	if isProtobuf {
		if err := proto.Unmarshal(body, &data); err != nil {
			return BadRequest("cannot unmarshal request body: %v", err)
		}
	} else {
		if err := protojson.Unmarshal(body, &data); err != nil {
			return BadRequest("cannot unmarshal request body: %v", err)
		}
	}

	serviceMgr := overlordServiceManager(c.d.overlord)
	for _, resourceLogs := range data.GetResourceLogs() {
		for _, scopeLogs := range resourceLogs.GetScopeLogs() {
			for _, record := range scopeLogs.GetLogRecords() {
				t := otlpRecordTime(record)
				message := otlpAnyValueToString(record.GetBody())
				serviceMgr.WriteServiceLog(serviceName, t, message)
			}
		}
	}

	return otlpEmptyResponse{protobuf: isProtobuf}
}

// v1PostOTLPMetrics implements the local OTLP/HTTP metrics receiver endpoint,
// POST /v1/services/{name}/otlp/v1/metrics. It accepts a JSON or Protobuf
// encoded ExportMetricsServiceRequest, and buffers the resource metrics for
// forwarding to any metrics-targets the named service is enrolled in.
func v1PostOTLPMetrics(c *Command, r *http.Request, _ *UserState) Response {
	serviceName := muxVars(r)["name"]

	contentType := r.Header.Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = contentType
	}

	var isProtobuf bool
	switch mediaType {
	case otlpContentTypeJSON:
		isProtobuf = false
	case otlpContentTypeProtobuf:
		isProtobuf = true
	default:
		return UnsupportedMediaType("unsupported content type %q, must be %q or %q",
			contentType, otlpContentTypeJSON, otlpContentTypeProtobuf)
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxOTLPBodyBytes+1))
	if err != nil {
		return BadRequest("cannot read request body: %v", err)
	}
	if len(body) > maxOTLPBodyBytes {
		return BadRequest("request body exceeds maximum size of %d bytes", maxOTLPBodyBytes)
	}

	var data metricspb.MetricsData
	if isProtobuf {
		if err := proto.Unmarshal(body, &data); err != nil {
			return BadRequest("cannot unmarshal request body: %v", err)
		}
	} else {
		if err := protojson.Unmarshal(body, &data); err != nil {
			return BadRequest("cannot unmarshal request body: %v", err)
		}
	}

	metricsMgr := overlordMetricsManager(c.d.overlord)
	metricsMgr.AddMetrics(serviceName, data.GetResourceMetrics())

	return otlpEmptyResponse{protobuf: isProtobuf}
}

// v1PostOTLPTraces implements the local OTLP/HTTP trace receiver endpoint,
// POST /v1/services/{name}/otlp/v1/traces. It accepts a JSON or Protobuf
// encoded ExportTraceServiceRequest, and buffers the resource spans for
// forwarding to any trace-targets the named service is enrolled in.
func v1PostOTLPTraces(c *Command, r *http.Request, _ *UserState) Response {
	serviceName := muxVars(r)["name"]

	contentType := r.Header.Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = contentType
	}

	var isProtobuf bool
	switch mediaType {
	case otlpContentTypeJSON:
		isProtobuf = false
	case otlpContentTypeProtobuf:
		isProtobuf = true
	default:
		return UnsupportedMediaType("unsupported content type %q, must be %q or %q",
			contentType, otlpContentTypeJSON, otlpContentTypeProtobuf)
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxOTLPBodyBytes+1))
	if err != nil {
		return BadRequest("cannot read request body: %v", err)
	}
	if len(body) > maxOTLPBodyBytes {
		return BadRequest("request body exceeds maximum size of %d bytes", maxOTLPBodyBytes)
	}

	var data tracepb.TracesData
	if isProtobuf {
		if err := proto.Unmarshal(body, &data); err != nil {
			return BadRequest("cannot unmarshal request body: %v", err)
		}
	} else {
		if err := protojson.Unmarshal(body, &data); err != nil {
			return BadRequest("cannot unmarshal request body: %v", err)
		}
	}

	traceMgr := overlordTraceManager(c.d.overlord)
	traceMgr.AddTraces(serviceName, data.GetResourceSpans())

	return otlpEmptyResponse{protobuf: isProtobuf}
}

// otlpRecordTime returns the best available timestamp for the given OTLP record.
func otlpRecordTime(record *logspb.LogRecord) time.Time {
	if nanos := record.GetTimeUnixNano(); nanos != 0 {
		return time.Unix(0, int64(nanos)).UTC()
	}
	if nanos := record.GetObservedTimeUnixNano(); nanos != 0 {
		return time.Unix(0, int64(nanos)).UTC()
	}
	return time.Now().UTC()
}

// otlpAnyValueToString renders an OTLP AnyValue (used for a LogRecord's body)
// as a string.
func otlpAnyValueToString(v *commonpb.AnyValue) string {
	if v == nil {
		return ""
	}
	switch value := v.GetValue().(type) {
	case *commonpb.AnyValue_StringValue:
		return value.StringValue
	case *commonpb.AnyValue_BoolValue:
		return strconv.FormatBool(value.BoolValue)
	case *commonpb.AnyValue_IntValue:
		return strconv.FormatInt(value.IntValue, 10)
	case *commonpb.AnyValue_DoubleValue:
		return strconv.FormatFloat(value.DoubleValue, 'g', -1, 64)
	case *commonpb.AnyValue_BytesValue:
		return string(value.BytesValue)
	case nil:
		return ""
	default:
		b, err := protojson.Marshal(v)
		if err != nil {
			return ""
		}
		return string(b)
	}
}

// otlpEmptyResponse serves the empty Export*ServiceResponse body mandated
// by the OTLP protocol on success.
type otlpEmptyResponse struct {
	protobuf bool
}

func (o otlpEmptyResponse) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if o.protobuf {
		w.Header().Set("Content-Type", otlpContentTypeProtobuf)
		w.WriteHeader(http.StatusOK)
		return
	}
	w.Header().Set("Content-Type", otlpContentTypeJSON)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("{}"))
}
