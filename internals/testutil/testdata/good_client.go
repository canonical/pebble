// Copyright (c) 2025 Canonical Ltd
//
// Licensed under GPLv3.

package testdata

import "net/http"

// GoodClient has CheckRedirect configured (should pass)
func GoodClient() *http.Client {
	return &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return nil
		},
	}
}
