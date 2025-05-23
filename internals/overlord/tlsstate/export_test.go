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

package tlsstate

import (
	"time"
)

// FakeTimeNow fakes the system time for the TLS manager.
func FakeTimeNow(t time.Time) (restore func()) {
	old := timeNow
	timeNow = func() time.Time {
		return t
	}
	return func() {
		timeNow = old
	}
}

// FakeIDCertValidity fakes the validity period of the identity certificate.
func FakeIDCertValidity(d time.Duration) (restore func()) {
	old := idCertValidity
	idCertValidity = d
	return func() {
		idCertValidity = old
	}
}

// FakeTLSCertValidity fakes the validity period of the TLS certificate (and private key).
func FakeTLSCertValidity(d time.Duration) (restore func()) {
	old := tlsCertValidity
	tlsCertValidity = d
	return func() {
		tlsCertValidity = old
	}
}

// FakeTLSCertRenewWindow fakes the grace period towards the end of the expiry date
// after which the TLS manager will consider it expired.
func FakeTLSCertRenewWindow(d time.Duration) (restore func()) {
	old := tlsCertRenewWindow
	tlsCertRenewWindow = d
	return func() {
		tlsCertRenewWindow = old
	}
}

var DefaultCertSubject = defaultCertSubject
