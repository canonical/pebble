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

// createHTTPClient creates an HTTP client with optional TLS support.
func createHTTPClient(transport *http.Transport) *http.Client {
	return &http.Client{Transport: transport}
}

// websocketScheme returns the appropriate WebSocket scheme for the HTTP scheme.
// In non-FIPS builds, "https" maps to "wss", everything else to "ws".
func websocketScheme(httpScheme string) string {
	if httpScheme == "https" {
		return "wss"
	}
	return "ws"
}

// createTLSTransport creates a transport with TLS configuration for non-FIPS builds.
// The baseURL parameter is accepted for consistency with the FIPS implementation.
func createTLSTransport(opts *Config, baseURL *url.URL) (*http.Transport, error) {
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
	}, nil
}
