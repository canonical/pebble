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

package state_test

// FIPS build: certificate authentication is not supported.
const certAuthSupported = false

// Error message for when no identity type is specified (FIPS).
const noTypeErrorMsg = `identity must have at least one type \("local" or "basic"; cert auth not supported in FIPS builds\)`

// Error messages for certificate validation (FIPS).
const (
	certPEMRequiredError = `certificate authentication is not supported in FIPS builds`
	certParseError       = `certificate authentication is not supported in FIPS builds`
	certExtraDataError   = `certificate authentication is not supported in FIPS builds`
)
