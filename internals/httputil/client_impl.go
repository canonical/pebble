//go:build !fips

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
	"net/http"
)

// validateScheme allows both HTTP and HTTPS in non-FIPS builds.
func validateScheme(scheme string) error {
	// All schemes allowed in non-FIPS builds
	return nil
}

// checkRedirectPolicy returns the default redirect policy (follow up to 10 redirects).
func checkRedirectPolicy() func(*http.Request, []*http.Request) error {
	// Use default behavior (nil means follow up to 10 redirects)
	return nil
}
