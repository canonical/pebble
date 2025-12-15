// Copyright (c) 2025 Canonical Ltd
//
// Licensed under GPLv3.

package testdata

import "net/http"

// BadClient has no CheckRedirect (should be detected as violation)
func BadClient() *http.Client {
	return &http.Client{
		Timeout: 10,
	}
}
