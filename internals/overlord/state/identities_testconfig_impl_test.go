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

package state_test

// Non-FIPS build: certificate authentication is supported.
const certAuthSupported = true

// Error message for when no identity type is specified in the default build.
const noTypeErrorMsg = `identity must have at least one type \("local", "basic", or "cert"\)`

// Error messages for certificate validation in the default build.
const (
	certPEMRequiredError = `cert identity must include a PEM-encoded certificate`
	certParseError       = `cannot parse certificate from cert identity: x509: .*`
	certExtraDataError   = `cert identity cannot have extra data after the PEM block`
)
