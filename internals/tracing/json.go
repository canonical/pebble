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

package tracing

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"google.golang.org/protobuf/encoding/protojson"

	tracepb "github.com/canonical/pebble/internals/otlp/trace/v1"
)

// marshalJSON encodes data using the OTLP/HTTP JSON encoding. See:
// https://opentelemetry.io/docs/specs/otlp/#json-protobuf-encoding
//
// This is the standard protobuf JSON mapping, with lowerCamelCase field
// names, except that:
//
//   - Trace and span IDs are hex strings, rather than the base64 strings
//     used for other bytes fields.
//   - Enum values are integers, rather than names.
//
// The protobuf JSON encoder handles enums, but not the IDs, so its output is
// decoded and the IDs rewritten.
func marshalJSON(data *tracepb.TracesData) ([]byte, error) {
	b, err := protojson.MarshalOptions{UseEnumNumbers: true}.Marshal(data)
	if err != nil {
		return nil, err
	}

	// Decode numbers as json.Number so they're re-encoded unchanged.
	var doc map[string]any
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.UseNumber()
	if err := decoder.Decode(&doc); err != nil {
		return nil, err
	}

	for _, rs := range objects(doc["resourceSpans"]) {
		for _, ss := range objects(rs["scopeSpans"]) {
			for _, span := range objects(ss["spans"]) {
				if err := hexIDs(span, "traceId", "spanId", "parentSpanId"); err != nil {
					return nil, err
				}
				for _, link := range objects(span["links"]) {
					if err := hexIDs(link, "traceId", "spanId"); err != nil {
						return nil, err
					}
				}
			}
		}
	}
	return json.Marshal(doc)
}

// objects returns the JSON objects in v, if it's an array.
func objects(v any) []map[string]any {
	array, _ := v.([]any)
	objects := make([]map[string]any, 0, len(array))
	for _, elem := range array {
		if obj, ok := elem.(map[string]any); ok {
			objects = append(objects, obj)
		}
	}
	return objects
}

// hexIDs re-encodes the named base64 fields of obj as hex. Fields that are
// absent, because they're empty, are left absent.
func hexIDs(obj map[string]any, fields ...string) error {
	for _, field := range fields {
		v, ok := obj[field].(string)
		if !ok {
			continue
		}
		id, err := base64.StdEncoding.DecodeString(v)
		if err != nil {
			return fmt.Errorf("invalid %s %q: %w", field, v, err)
		}
		obj[field] = hex.EncodeToString(id)
	}
	return nil
}
