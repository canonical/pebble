//go:build fips

// Copyright (c) 2021 Canonical Ltd
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

package checkstate

import (
	"fmt"
	"net/http"
	"strings"
)

// checkHTTPSURL blocks HTTPS URLs in FIPS builds.
func checkHTTPSURL(url string) error {
	if strings.HasPrefix(strings.ToLower(url), "https://") {
		return fmt.Errorf("HTTPS health checks are not supported in FIPS builds")
	}
	return nil
}

// createHTTPClient creates an HTTP client that blocks HTTPS redirects in FIPS builds.
func createHTTPClient() *http.Client {
	return &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if strings.ToLower(req.URL.Scheme) == "https" {
				return fmt.Errorf("HTTPS redirects are not supported in FIPS builds")
			}
			// Default behaviour in src/net/http/client.go
			if len(via) >= 10 {
				return fmt.Errorf("stopped after 10 redirects")
			}
			return nil
		},
	}
}
