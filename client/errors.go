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

package client

// Error is the real value of response.Result when an error type response occurs.
type Error struct {
	Message string      `json:"message"`
	Kind    ErrorKind   `json:"kind,omitempty"`
	Value   interface{} `json:"value,omitempty"`
}

func (e *Error) Error() string {
	return e.Message
}

type ErrorKind string

// Error kinds for use as a response result.
const (
	ErrorKindLoginRequired     = ErrorKind("login-required")
	ErrorKindNoDefaultServices = ErrorKind("no-default-services")
	ErrorKindNotFound          = ErrorKind("not-found")
	ErrorKindPermissionDenied  = ErrorKind("permission-denied")
	ErrorKindGenericFileError  = ErrorKind("generic-file-error")
)

// Maintenance error kinds for use only inside the maintenance field of responses.
const (
	ErrorKindSystemRestart = ErrorKind("system-restart")
	ErrorKindDaemonRestart = ErrorKind("daemon-restart")
)
