//go:build !fips

// Copyright (c) 2014-2020 Canonical Ltd
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

package state

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
)

// marshalCertIdentity encodes a certificate identity to PEM format for serialization.
func marshalCertIdentity(cert *CertIdentity) *marshalledCertIdentity {
	pemBlock := &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: cert.X509.Raw,
	}
	return &marshalledCertIdentity{PEM: string(pem.EncodeToMemory(pemBlock))}
}

// unmarshalCertIdentity decodes a PEM-encoded certificate identity.
func unmarshalCertIdentity(mi *marshalledCertIdentity) (*CertIdentity, error) {
	block, _ := pem.Decode([]byte(mi.PEM))
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("cannot parse certificate from cert identity: %w", err)
	}
	return &CertIdentity{X509: cert}, nil
}
