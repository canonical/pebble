//go:build fips

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

package client

import (
	"fmt"
	"net/http"
	"net/url"
)

// validateBaseURL checks if the base URL is valid for FIPS builds.
// HTTPS is not allowed in FIPS builds.
func validateBaseURL(baseURL *url.URL) error {
	if baseURL.Scheme == "https" {
		return fmt.Errorf("HTTPS is not supported in FIPS builds")
	}
	return nil
}

// createHTTPClient creates an HTTP client with a redirect policy that blocks HTTPS.
func createHTTPClient(transport *http.Transport) *http.Client {
	return &http.Client{
		Transport:     transport,
		CheckRedirect: checkRedirectPolicy(),
	}
}

// createTLSTransport is not supported in FIPS builds and will panic if called.
// This should never be reached due to validateBaseURL blocking HTTPS URLs.
func createTLSTransport(opts *Config) *http.Transport {
	panic("HTTPS transport is not supported in FIPS builds")
}

// checkRedirectPolicy returns a redirect policy that blocks redirects to HTTPS.
func checkRedirectPolicy() func(req *http.Request, via []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "http" {
			return fmt.Errorf("redirects are supported only to HTTP addresses in FIPS builds")
		}
		// Use Go runtime default redirect limit of ten.
		if len(via) >= 10 {
			return fmt.Errorf("stopped after 10 redirects")
		}
		return nil
	}
}
