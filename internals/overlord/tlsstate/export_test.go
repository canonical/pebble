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

package tlsstate

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"slices"
	"time"
)

// TLSKeyPairsAll does not apply any validity filtering.
func (m TLSManager) TLSKeyPairsAll() []*TLSKeyPair {
	return m.tlsKeyPairs
}

func (m *TLSManager) AddNewTLSKeypair(notBefore time.Time, notAfter time.Time) (*TLSKeyPair, error) {
	return m.addNewTLSKeyPair(notBefore, notAfter)
}

func (m TLSManager) IsTLSKeypairSupported(keypair *TLSKeyPair) bool {
	return m.isTLSKeyPairSupported(keypair)
}

func (t TLSKeyPair) IsExpired() bool {
	return t.isExpired()
}

// FakeSystemTime allows faking the time.
func (m *TLSManager) FakeSystemTime(date string, offset time.Duration) (restore func(), clock time.Time) {
	layout := "2006-01-02"
	now, err := time.Parse(layout, date)
	if err != nil {
		panic("invalid date string")
	}
	now = now.Add(offset)
	old := systemTime
	systemTime = func() time.Time {
		return now
	}
	return func() {
		systemTime = old
	}, now
}

// FakeCertificateValidityPeriod allows setting how long a new certificate will be valid for.
func (m *TLSManager) FakeCertificateValidityPeriod(valid time.Duration) (restore func()) {
	old := certificateValidityPeriod
	certificateValidityPeriod = valid
	return func() {
		certificateValidityPeriod = old
	}
}

// AddNewUnsupportedTLSKeyPair installs an unsupported ECDSA NIST P-224
// based keypair for testing purposes.
func (m *TLSManager) AddNewUnsupportedTLSKeyPair(notBefore time.Time, notAfter time.Time) (*TLSKeyPair, error) {
	// Generate
	keypair, err := m.genX509ECP224Keypair(notBefore, notAfter)
	if err != nil {
		return nil, err
	}
	// Persist
	err = m.persistTLSKeyPair(keypair)
	if err != nil {
		return nil, err
	}
	m.tlsKeyPairs = append(m.tlsKeyPairs, keypair)
	// Sort by valid starting date, end date and fingerprint.
	slices.SortFunc(m.tlsKeyPairs, func(a, b *TLSKeyPair) int {
		return a.Compare(b)
	})
	return keypair, nil
}

// genX509ECP224Keypair generates a new keypair based on ECDSA NIST P-224
// private key and X509 public certificate. The certificate is signed with
// the same private key (self-signed), which means that the public key
// in the certificate can be used to verify the signature.
func (m *TLSManager) genX509ECP224Keypair(notBefore time.Time, notAfter time.Time) (*TLSKeyPair, error) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P224(), rand.Reader)
	if err != nil {
		return nil, err
	}
	// Random serial for now.
	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return nil, err
	}
	template := x509.Certificate{
		SerialNumber: serialNumber,
		NotBefore:    notBefore,
		NotAfter:     notAfter,
		Subject: pkix.Name{
			Organization: []string{"Canonical Ltd."},
		},
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	// DER encoded bytes
	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return nil, err
	}
	// Load a clean X509 certificate from the DER encoded bytes.
	certificate, err := x509.ParseCertificate(derBytes)
	if err != nil {
		return nil, err
	}
	keypair := &TLSKeyPair{privateKey: privateKey, Certificate: certificate}
	keypair.addFingerprint()
	return keypair, nil
}
