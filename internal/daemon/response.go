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
	"path/filepath"

	"github.com/canonical/pebble/internal/logger"
	"time"
)

type ResponseType string

const (
	ResponseTypeSync  ResponseType = "sync"
	ResponseTypeAsync ResponseType = "async"
	ResponseTypeError ResponseType = "error"
)

// Response knows how to serve itself, and how to find itself
type Response interface {
	ServeHTTP(w http.ResponseWriter, r *http.Request)
}

type resp struct {
	Status           int          `json:"status-code"`
	Type             ResponseType `json:"type"`
	Change           string       `json:"change,omitempty"`
	Result           interface{}  `json:"result,omitempty"`
	WarningTimestamp *time.Time   `json:"warning-timestamp,omitempty"`
	WarningCount     int          `json:"warning-count,omitempty"`
	Maintenance      *errorResult `json:"maintenance,omitempty"`
}

type respJSON struct {
	Type             ResponseType `json:"type"`
	Status           int          `json:"status-code"`
	StatusText       string       `json:"status,omitempty"`
	Change           string       `json:"change,omitempty"`
	Result           interface{}  `json:"result,omitempty"`
	WarningTimestamp *time.Time   `json:"warning-timestamp,omitempty"`
	WarningCount     int          `json:"warning-count,omitempty"`
	Maintenance      *errorResult `json:"maintenance,omitempty"`
}

func (r *resp) transmitMaintenance(kind errorKind, message string) {
	r.Maintenance = &errorResult{
		Kind:    kind,
		Message: message,
	}
}

func (r *resp) addWarningsToMeta(count int, stamp time.Time) {
	if r.WarningCount != 0 {
		return
	}
	if count == 0 {
		return
	}
	r.WarningCount = count
	r.WarningTimestamp = &stamp
}

func (r *resp) MarshalJSON() ([]byte, error) {
	return json.Marshal(respJSON{
		Type:             r.Type,
		Status:           r.Status,
		StatusText:       http.StatusText(r.Status),
		Change:           r.Change,
		Result:           r.Result,
		WarningTimestamp: r.WarningTimestamp,
		WarningCount:     r.WarningCount,
		Maintenance:      r.Maintenance,
	})
}

func (r *resp) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	status := r.Status
	bs, err := r.MarshalJSON()
	if err != nil {
		logger.Noticef("cannot marshal %#v to JSON: %v", *r, err)
		bs = nil
		status = 500
	}

	hdr := w.Header()
	if r.Status == 202 || r.Status == 201 {
		if m, ok := r.Result.(map[string]interface{}); ok {
			if location, ok := m["resource"]; ok {
				if location, ok := location.(string); ok && location != "" {
					hdr.Set("Location", location)
				}
			}
		}
	}

	hdr.Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(bs)
}

type errorKind string

const (
	errorKindLoginRequired = errorKind("login-required")
	errorKindDaemonRestart = errorKind("daemon-restart")
	errorKindSystemRestart = errorKind("system-restart")
)

type errorValue interface{}

type errorResult struct {
	Message string     `json:"message"` // note no omitempty
	Kind    errorKind  `json:"kind,omitempty"`
	Value   errorValue `json:"value,omitempty"`
}

func SyncResponse(result interface{}) Response {
	if err, ok := result.(error); ok {
		return statusInternalError("internal error: %v", err)
	}

	if rsp, ok := result.(Response); ok {
		return rsp
	}

	return &resp{
		Type:   ResponseTypeSync,
		Status: 200,
		Result: result,
	}
}

func AsyncResponse(result map[string]interface{}) Response {
	return &resp{
		Type:   ResponseTypeAsync,
		Status: 202,
		Result: result,
	}
}

// A fileResponse 's ServeHTTP method serves the file
type fileResponse string

// ServeHTTP from the Response interface
func (f fileResponse) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	filename := fmt.Sprintf("attachment; filename=%s", filepath.Base(string(f)))
	w.Header().Add("Content-Disposition", filename)
	http.ServeFile(w, r, string(f))
}

func makeErrorResponder(status int) errorResponder {
	return func(format string, v ...interface{}) Response {
		res := &errorResult{}
		if len(v) == 0 {
			res.Message = format
		} else {
			res.Message = fmt.Sprintf(format, v...)
		}
		if status == 401 {
			res.Kind = errorKindLoginRequired
		}
		return &resp{
			Type:   ResponseTypeError,
			Result: res,
			Status: status,
		}
	}
}

// errorResponder is a callable that produces an error Response.
// e.g., InternalError("something broke: %v", err), etc.
type errorResponder func(string, ...interface{}) Response

// Standard error responses.
var (
	statusUnauthorized     = makeErrorResponder(401)
	statusNotFound         = makeErrorResponder(404)
	statusBadRequest       = makeErrorResponder(400)
	statusMethodNotAllowed = makeErrorResponder(405)
	statusInternalError    = makeErrorResponder(500)
	statusNotImplemented   = makeErrorResponder(501)
	statusForbidden        = makeErrorResponder(403)
	statusConflict         = makeErrorResponder(409)
)
