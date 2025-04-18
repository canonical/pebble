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

// tlsstate manages TLS keypairs on behalf of the daemon's HTTPS server.
package tlsstate

import (
	"bytes"
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"math/big"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	// systemTime can be faked during testing.
	systemTime = time.Now

	// idCertValidity sets the x509 identity key validity period. The
	// certificate is managed by the TLS manager as it is not directly
	// supported by a crypto.Signer interface, as its an implementation
	// detail related to TLS (not a signing key). Expiry of this
	// certificate does not change the identity, but changes the client
	// pinned identity certificate, forcing the client to re-pair with
	// the server.
	idCertValidity = 10 * 365 * 24 * time.Hour

	// tlsCertValidity defines how long the in-memory TLS keypairs are
	// valid before a new keypair is generated. This allows for
	// TLS session resumption during this period, and keypair re-use
	// from concurrent requests. The manager restart will always result
	// in the creation of a new TLS keypair.
	tlsCertValidity = time.Hour

	// tlsCertRenewWindow defines how long from the actual certificate
	// expiry we should rather rotate in order to avoid a race between
	// the expiry and the session handshake completing in time.
	tlsCertRenewWindow = 60 * time.Second

	// idCertFile is the public x509 certificate, which holds the
	// identity public key, and self-signed with the identity key.
	idCertFile = "identity.pem"
)

type TLSManager struct {
	// tlsDir is the location of the PEM keypair files.
	tlsDir string
	mu     sync.RWMutex
	// The active in-memory TLS certificate (keypair).
	tlsCert *tls.Certificate
	// The identity certificate loaded from disk.
	idCert *x509.Certificate
	// The identity key used for signing TLS certificates.
	signer crypto.Signer
}

// NewManager create a new TLS keypair manager. The tlsDir must be a
// directory of which only the leaf element, at most, does not exist. The
// leaf directory will be created with 0o700 permissions. The signer
// must implement a crypto.Signer interface. The signer represents the
// identity key of the machine, container or device. The signer will be
// used to sign TLS keypairs and its certificate (CA) is included in the
// TLS certificate chain, allowing a client to pin the identity
// certificate once trust has been manually established as part of a
// trust exchange procedure.
func NewManager(tlsDir string, signer crypto.Signer) (*TLSManager, error) {
	manager := &TLSManager{
		tlsDir: tlsDir,
		signer: signer,
	}
	if err := manager.createDir(); err != nil {
		return nil, fmt.Errorf("cannot create TLS directory: %w", err)
	}
	return manager, nil
}

// GetCertificate returns a identity signed TLS certificate. The certificate chain includes
// both the TLS leaf certificate, as well as the self signed identity CA certificate. If
// either the identity or TLS certificate nears expiry, this functions creates new
// certificates on demand. Note that even if the identity certificate is re-created, this
// does not mean that the identity key changed.
func (m *TLSManager) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	var err error

	// Concurrent sessions while the ID and TLS certificate is valid.
	m.mu.RLock()
	tlsCert := m.tlsCert
	idCert := m.idCert
	m.mu.RUnlock()
	if idCert != nil && isCertActive(idCert) && tlsCert != nil && isCertActive(tlsCert.Leaf) {
		return tlsCert, nil
	}

	// Generate a new in-memory identity signed TLS certificate.
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.createIDCert(); err != nil {
		return nil, fmt.Errorf("cannot create identity certificate: %w", err)
	}
	if err = m.createTLSCert(); err != nil {
		return nil, fmt.Errorf("cannot create TLS certificate: %w", err)
	}
	return m.tlsCert, nil
}

func (m *TLSManager) createTLSCert() error {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return err
	}
	template := x509.Certificate{
		SerialNumber:          serialNumber,
		NotBefore:             systemTime(),
		NotAfter:              systemTime().Add(tlsCertValidity),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	// DER encoded bytes
	derBytes, err := x509.CreateCertificate(rand.Reader, &template, m.idCert, privateKey.Public(), m.signer)
	if err != nil {
		return err
	}
	// Load a clean X509 certificate from the DER encoded bytes.
	certificate, err := x509.ParseCertificate(derBytes)
	if err != nil {
		return err
	}
	m.tlsCert = &tls.Certificate{
		PrivateKey: privateKey,
		// Leaf first, then CA cert.
		Certificate: [][]byte{certificate.Raw, m.idCert.Raw},
		Leaf:        certificate,
	}
	return nil
}

// createDir verifies the directory layout and permissions, and creates
// the leaf element of the TLS driectory if it does not yet exist.
func (m *TLSManager) createDir() error {
	exists, err := pathExists(m.tlsDir)
	if err != nil {
		return err
	}
	if exists {
		return expectPermission(m.tlsDir, 0o700)
	}

	// Create the leaf directory node with 0o700 permissions.
	return os.Mkdir(m.tlsDir, 0o700)
}

// createIDCert verifies that the identity certificate exists, and that the
// certificate matches the supplied identity key signer. If the identity key
// has changed, or the certificate has expired, the identity certificate is
// replaced with a new identity certificate signed with the identity key.
func (m *TLSManager) createIDCert() error {
	idPath := filepath.Join(m.tlsDir, idCertFile)
	exists, err := pathExists(idPath)
	if err != nil {
		return err
	}

	// Make sure the current certificate is valid.
	if exists {
		cert, err := loadIDCert(idPath)
		if err != nil {
			return err
		}
		isDerived := isCertDerived(cert, m.signer)
		isActive := isCertActive(cert)
		if isDerived && isActive {
			// Existing identity certificate is valid.
			m.idCert = cert
			return nil
		}
	}

	// If we get here a new identity certificate must be created.
	cert, err := createIDCert(m.signer)
	if err != nil {
		return err
	}
	err = saveIDCert(idPath, cert)
	if err != nil {
		return err
	}
	m.idCert = cert
	return nil
}

// loadIDCert loads the x509 identity certificate, which contains
// the identity public key. We only allow a single certificate
// block in the PEM file.
func loadIDCert(path string) (*x509.Certificate, error) {
	var cert *x509.Certificate
	pemData, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	blockCount := 0
	for {
		var block *pem.Block
		block, pemData = pem.Decode(pemData)
		if block == nil {
			if blockCount == 1 {
				break
			}
			return nil, fmt.Errorf("empty PEM file")
		}
		switch block.Type {
		case "CERTIFICATE":
			// We only support a single certificate block.
			if blockCount == 1 {
				return nil, fmt.Errorf("unexpected PEM block")
			}
			cert, err = x509.ParseCertificate(block.Bytes)
			if err != nil {
				return nil, err
			}
			blockCount += 1
		default:
			return nil, fmt.Errorf("unexpected PEM block")
		}
	}
	return cert, nil
}

// saveIDCert saves the x509 identity certificate, which contains
// the identity public key.
func saveIDCert(path string, cert *x509.Certificate) error {
	certPEMBlock := &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: cert.Raw,
	}
	var pemBuffer bytes.Buffer
	if err := pem.Encode(&pemBuffer, certPEMBlock); err != nil {
		return err
	}
	pemBytes := pemBuffer.Bytes()

	pemFile, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, pemFile.Sync())
		err = errors.Join(err, pemFile.Close())
	}()

	if _, err = pemFile.Write(pemBytes); err != nil {
		return err
	}
	return nil
}

// createIDCert create a public x509 CA certificate, signed by the identity
// key. This certificate is included in the TLS certificate chain as the
// non-leaf certificate, allowing the client to pin this certificate during
// the client server trust exchange (pairing) procedure.
func createIDCert(signer crypto.Signer) (*x509.Certificate, error) {
	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return nil, err
	}
	template := &x509.Certificate{
		SerialNumber:          serialNumber,
		NotBefore:             systemTime(),
		NotAfter:              systemTime().Add(idCertValidity),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{},
		BasicConstraintsValid: true,
		IsCA:                  true,
		// We can only sign leaf certificates with this.
		MaxPathLen:     0,
		MaxPathLenZero: true,
	}

	// The identity CA cert is self-signed, which allows a client to verify the
	// incoming version of this with the pinned version.
	certDER, err := x509.CreateCertificate(rand.Reader, template, template, signer.Public(), signer)
	if err != nil {
		return nil, err
	}
	// Load a clean X509 certificate from the DER encoded bytes.
	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return nil, err
	}
	return cert, nil
}

// isCertActive checks of the current time now is within the start and end time
// validity period as defined by the certificate.
func isCertActive(cert *x509.Certificate) bool {
	now := systemTime()
	// Note that we shorten the NotAfter timestamp by a tlsCertRenewWindow
	// duration to avoid a TLS handshake race against the expiry time.
	if now.Before(cert.NotBefore) || now.After(cert.NotAfter.Add(-tlsCertRenewWindow)) {
		return true
	}
	return false
}

// isCertDerived checks if the public key in the certificate matches
// the signer public key. This confirms the certificate matches the
// identity key.
func isCertDerived(cert *x509.Certificate, signer crypto.Signer) bool {
	signerPublic := signer.Public().(ed25519.PublicKey)
	certPublic := cert.PublicKey.(ed25519.PublicKey)
	return bytes.Equal(signerPublic, certPublic)
}

// expectPermission return an error if the specified directory or file
// path is not matching the supplied permissions.
func expectPermission(path string, perm fs.FileMode) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	actualPerm := info.Mode().Perm()
	if actualPerm != perm {
		return fmt.Errorf("expected permission 0o%o (got 0o%o) for %q", perm, actualPerm, path)
	}
	return nil
}

// pathExists returns true of the path exists, false if it does not. If the
// operation reports an unrelated error, the error is returned.
func pathExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
