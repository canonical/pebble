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

// createHTTPClient creates an HTTP client with a redirect policy that blocks HTTPS.
func createHTTPClient(transport *http.Transport) *http.Client {
	return &http.Client{
		Transport:     transport,
		CheckRedirect: checkRedirectPolicy(),
	}
}

// websocketScheme returns the appropriate WebSocket scheme for the HTTP scheme.
// In FIPS builds, only "ws" is supported (HTTPS/WSS not allowed).
func websocketScheme(httpScheme string) string {
	return "ws"
}

// createTLSTransport is not supported in FIPS builds.
// It validates the baseURL and returns an error if HTTPS is attempted.
func createTLSTransport(opts *Config, baseURL *url.URL) (*http.Transport, error) {
	if baseURL.Scheme != "http" {
		return nil, fmt.Errorf("Only the HTTP scheme is supported in FIPS builds")
	}
	// This should never be reached since we check for https scheme above,
	// but kept as a safeguard.
	panic("createTLSTransport called in FIPS build with non-HTTPS URL")
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
