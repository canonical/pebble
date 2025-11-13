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
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/canonical/pebble/internals/logger"
)

// checkHTTPSURL returns an error if URL starts with https:// in FIPS mode.
func checkHTTPSURL(url string) error {
	if strings.HasPrefix(strings.ToLower(url), "https://") {
		return fmt.Errorf("HTTPS health checks are not supported in FIPS mode")
	}
	return nil
}

// createHTTPClient creates an HTTP client that blocks HTTPS redirects in FIPS mode.
func createHTTPClient() *http.Client {
	return &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Block HTTPS redirects in FIPS mode
			if strings.ToLower(req.URL.Scheme) == "https" {
				return fmt.Errorf("HTTPS redirects are not supported in FIPS mode")
			}
			// Use default redirect behavior for HTTP
			if len(via) >= 10 {
				return fmt.Errorf("stopped after 10 redirects")
			}
			return nil
		},
	}
}

func (c *httpChecker) check(ctx context.Context) error {
	if err := checkHTTPSURL(c.url); err != nil {
		return err
	}

	logger.Debugf("Check %q (http): requesting %q", c.name, c.url)
	client := createHTTPClient()
	request, err := http.NewRequestWithContext(ctx, "GET", c.url, nil)
	if err != nil {
		return fmt.Errorf("cannot build request: %w", err)
	}
	for k, v := range c.headers {
		request.Header.Set(k, v)
	}

	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode > 299 {
		// Include first few lines of response body in error details
		output, err := io.ReadAll(io.LimitReader(response.Body, maxErrorBytes))
		details := ""
		if err != nil {
			details = fmt.Sprintf("cannot read response: %v", err)
		} else {
			lines := strings.Split(strings.TrimSpace(string(output)), "\n")
			if len(lines) > maxErrorLines {
				lines = lines[:maxErrorLines+1]
				lines[maxErrorLines] = "(...)"
			}
			details = strings.Join(lines, "\n")
		}
		return &detailsError{
			error:   fmt.Errorf("non-2xx status code %d", response.StatusCode),
			details: details,
		}
	}
	return nil
}
