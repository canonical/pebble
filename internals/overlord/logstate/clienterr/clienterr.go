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

/*
Package clienterr contains common error types which are recognised by the log
gatherer. logstate.logClient implementations should return these error types
to communicate with the gatherer.

Errors in this package should be pattern-matched using errors.As:

		err := client.Flush(ctx)
		backoff := &clienterr.Backoff{}
		if errors.As(err, &backoff) {
	      ...
		}
*/
package clienterr

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Backoff should be returned if the server indicates we are sending too many
// requests (e.g. an HTTP 429 response).
type Backoff struct {
	RetryAfter *time.Time
}

func (e *Backoff) Error() string {
	errStr := "too many requests"
	if e.RetryAfter != nil {
		errStr += ", retry after " + e.RetryAfter.String()
	}
	return errStr
}

// ErrorResponse represents an HTTP error response from the server
// (4xx or 5xx).
type ErrorResponse struct {
	StatusCode int
	Body       bytes.Buffer
	ReadErr    error
}

func (e *ErrorResponse) Error() string {
	errStr := fmt.Sprintf("server returned HTTP %d\n", e.StatusCode)
	if e.Body.Len() > 0 {
		errStr += fmt.Sprintf(`response body:
%s
`, e.Body.String())
	}
	if e.ReadErr != nil {
		errStr += "cannot read response body: " + e.ReadErr.Error()
	}
	return errStr
}

// ErrorFromResponse generates a *ErrorResponse from a failed *http.Response.
// NB: this function reads the response body.
func ErrorFromResponse(resp *http.Response) *ErrorResponse {
	err := &ErrorResponse{}
	err.StatusCode = resp.StatusCode
	_, readErr := io.CopyN(&err.Body, resp.Body, 1024)
	err.ReadErr = readErr
	return err
}
