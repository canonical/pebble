//go:build fips

// Copyright (c) 2024 Canonical Ltd
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

package httputil

import (
	"errors"
	"net/http"
)

// validateScheme blocks HTTPS in FIPS builds, allows HTTP and other schemes.
func validateScheme(scheme string) error {
	if scheme != "http" {
		return errors.New("Only the HTTP scheme is supported in FIPS builds")
	}
	return nil
}

// checkRedirectPolicy blocks HTTPS redirects in FIPS builds.
func checkRedirectPolicy() func(*http.Request, []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "http" {
			return errors.New("Only HTTP redirects are allowed in FIPS builds")
		}
		// Allow HTTP redirects up to 10 times (Go default)
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		return nil
	}
}
