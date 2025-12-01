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
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// ClientOptions specifies options for creating an HTTP client.
type ClientOptions struct {
	// Timeout for HTTP requests.
	Timeout time.Duration
}

// NewClient creates an HTTP client with the specified options.
// In FIPS builds, the client will block HTTPS URLs and HTTPS redirects.
func NewClient(opts ClientOptions) *http.Client {
	client := &http.Client{
		Timeout:       opts.Timeout,
		CheckRedirect: checkRedirectPolicy(),
	}
	return client
}

// ValidateURL validates that the URL is allowed in the current build mode.
// In FIPS builds, only HTTP URLs are allowed.
// In non-FIPS builds, both HTTP and HTTPS are allowed.
func ValidateURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	return validateScheme(u.Scheme)
}
