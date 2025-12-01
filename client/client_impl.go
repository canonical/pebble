//go:build !fips

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
	"crypto/tls"
	"net/http"
	"net/url"
)

// validateBaseURL checks if the base URL is valid for non-FIPS builds.
// HTTPS is allowed in non-FIPS builds.
func validateBaseURL(baseURL *url.URL) error {
	return nil
}

// createHTTPClient creates an HTTP client with optional TLS support.
func createHTTPClient(transport *http.Transport) *http.Client {
	return &http.Client{Transport: transport}
}

// createTLSTransport creates a transport with TLS configuration for non-FIPS builds.
func createTLSTransport(opts *Config) *http.Transport {
	return &http.Transport{
		DisableKeepAlives: opts.DisableKeepAlive,
		TLSClientConfig: &tls.Config{
			// We disable the internal full X509 metadata based validation logic
			// since the typical use-case do not have the server as a public URL
			// baked into the certificate, signed with an external CA. The client
			// config provides a TLSServerVerify hook that must be used to verify
			// the server certificate chain.
			InsecureSkipVerify: true,
			VerifyConnection: func(state tls.ConnectionState) error {
				return verifyConnection(state, opts)
			},
			// The server is configured to request a certificate from the client
			// which will result in this hook getting called to retrieve it.
			GetClientCertificate: func(request *tls.CertificateRequestInfo) (*tls.Certificate, error) {
				return opts.TLSClientIDCert, nil
			},
		},
	}
}

// checkRedirectPolicy returns a redirect policy that blocks redirects to HTTPS
// in FIPS builds. In non-FIPS builds, use the default policy (follow up to 10 redirects).
func checkRedirectPolicy() func(req *http.Request, via []*http.Request) error {
	return nil // Use default policy
}
