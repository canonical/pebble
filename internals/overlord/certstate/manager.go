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

// certstate manages key-pair and certificate creation and selection.
package certstate

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io/fs"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type CertManager struct {
	tlsDir           string
	x509KeyPairsLock sync.Mutex
	x509KeyPairs     []x509KeyPair
	changeListeners  []CertChangedFunc
}

// x509KeyPair represents the X509 public certificate and the private
// key that signed it.
type x509KeyPair struct {
	Order       int
	Private     any
	Certificate *x509.Certificate
}

func NewManager(tlsDir string) (*CertManager, error) {
	manager := &CertManager{tlsDir: tlsDir}
	if err := manager.loadX509KeyPairs(); err != nil {
		return nil, err
	}
	return manager, nil
}

// X509Keypair returns a valid tls.Certificate instance that
// can be used for starting a TLS based network listener. A call
// to this function may generate a new X509 keypair if none
// of the available pairs are valid.
func (c *CertManager) X509Keypair() (*tls.Certificate, error) {
	c.x509KeyPairsLock.Lock()
	defer c.x509KeyPairsLock.Unlock()

	var selected *x509KeyPair

	// Try to find an existing valid X509
	for i, keypair := range c.x509KeyPairs {
		if !isCertExpired(keypair.Certificate) {
			selected = &c.x509KeyPairs[i]
			break
		}
	}

	// Generate a keypair if no one exists.
	if selected == nil {
		selected, err := c.newX509ECP256Keypair()
		if err != nil {
			return nil, fmt.Errorf("cannot generate X509 keypair: %w", err)
		}
		if err := c.persistX509Keypair(selected); err != nil {
			return nil, fmt.Errorf("cannot persist X509 keypair: %w", err)
		}

		// Notify subscribers of the new certificate in use
		c.callChangeListeners(selected.Certificate)
	}

	pemCert, err := x509CertToPEM(selected.Certificate)
	if err != nil {
		return nil, err
	}
	pemKey, err := privateKeyToPEM(selected.Private)
	if err != nil {
		return nil, err
	}
	tlsCert, err := tls.X509KeyPair(pemCert, pemKey)
	if err != nil {
		return nil, err
	}
	return &tlsCert, err
}

// CertChangedFunc is the function type used by AddChangeListener.
type CertChangedFunc func(cert *x509.Certificate)

// AddChangeListener adds f to the list of functions that are called
// whenever a cert change event took place (for example, when an
// expired certificate gets replaced).
func (c *CertManager) AddChangeListener(f CertChangedFunc) {
	c.changeListeners = append(c.changeListeners, f)
}

func (c *CertManager) callChangeListeners(cert *x509.Certificate) {
	if cert == nil {
		// Avoids if statement on every deferred call to this method (we
		// shouldn't call listeners when the operation fails).
		return
	}
	for _, f := range c.changeListeners {
		f(cert)
	}
}

// newX509ECP256Keypair generates an elliptic P256 private key and an
// X509 self-signed public certificate (signed with the private key). The x509
// certificate includes both the public key and the signature, which allows
// the certificate signature to be verified with the public key inside the
// certificate. This allows a client side copy of the certificate to be used
// as certificate authority for future TLS sessions with the server.
func (c *CertManager) newX509ECP256Keypair() (*x509KeyPair, error) {
	// Get the highest available order
	order := 1
	certCount := len(c.x509KeyPairs)
	if certCount > 0 {
		// Find the next available order
		order = c.x509KeyPairs[certCount-1].Order + 1
		if order > 999 {
			return nil, fmt.Errorf("cannot process order number %v (valid range is 001-999)", order)
		}
	}

	// Valid date range.
	notBefore := time.Now()
	notAfter := notBefore.Add(365 * 24 * time.Hour)
	keypair, err := generateX509ECP256Keypair(notBefore, notAfter)
	if err != nil {
		return nil, err
	}
	keypair.Order = order
	return keypair, nil
}

func (c *CertManager) persistX509Keypair(keypair *x509KeyPair) error {
	return writeX509Keypair(keypair, c.tlsDir)
}

// PEMCertificates returns the slice of valid X509 certificates
// available at the point where the call is made. The result is
// a list of PEM encoded ceritficates.
func (c *CertManager) PEMCertificates() ([][]byte, error) {
	c.x509KeyPairsLock.Lock()
	defer c.x509KeyPairsLock.Unlock()

	var pemCerts [][]byte
	for _, keypair := range c.x509KeyPairs {
		if !isCertExpired(keypair.Certificate) {
			pem, err := x509CertToPEM(keypair.Certificate)
			if err != nil {
				return nil, err
			}
			pemCerts = append(pemCerts, pem)
		}
	}
	return pemCerts, nil
}

// Ensure implements StateManager.Ensure.
func (c *CertManager) Ensure() error {

	// TODO: Implement expired certificate purging, so that expired files
	// are removed, and the ordering recalculated so we do not run out of
	// order room.
	return nil
}

// loadX509KeyPairs loads all the persisted X509 keypairs, without
// changing the state of the system (it does not create new keypairs).
func (c *CertManager) loadX509KeyPairs() error {
	// If the directory does not exist that simply means no keypairs
	// are currently available, and that is OK.
	_, err := os.Stat(c.tlsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
	}

	err = expectPermission(c.tlsDir, 0o700)
	if err != nil {
		return fmt.Errorf("cannot verify X509 keypair directory permission: %w", err)
	}

	// Every valid keypair must provide two files, one for the private key
	// (<order>-key.pem), and another for the X509 certificate
	// (<order>-cert.pem). The order ranges from 001-999. Entries are
	// ordered alphanumerically, due to os.ReadDir.
	entries, err := os.ReadDir(c.tlsDir)
	if err != nil {
		return fmt.Errorf("cannot read X509 keypair directory: %v", err)
	}

	// skipNext indicates we already processed the certificate and the following private key file.
	var skipNext bool

	for index, entry := range entries {
		if skipNext || (!strings.HasSuffix(entry.Name(), "cert.pem") && !strings.HasSuffix(entry.Name(), "key.pem")) {
			skipNext = false
			continue
		}

		// Get the order.
		orderPrefix, _, _ := strings.Cut(entry.Name(), "-")
		order, err := strconv.Atoi(orderPrefix)
		if err != nil {
			return fmt.Errorf("cannot extract order number prefix from %q: %w", entry.Name(), err)
		}
		if order <= 0 || order > 999 {
			return fmt.Errorf("cannot process order number %v (valid range is 001-999)", order)
		}

		// See if both requires PEM files are present.
		if !strings.HasSuffix(entry.Name(), "cert.pem") ||
			len(entries) < (index+2) ||
			!strings.HasSuffix(entries[index+1].Name(), fmt.Sprintf("%03d-key.pem", order)) {
			// 1. The first file we encountered alphanumerically is not the <order>-cert.pem
			// 2. The list of entries stops here
			// 3. The next entry is not the expected order matching private key
			return fmt.Errorf("cannot find the expected X509 certificate or its private key")
		}

		certPath := filepath.Join(c.tlsDir, entry.Name())
		keyPath := filepath.Join(c.tlsDir, fmt.Sprintf("%3d-key.pem", order))

		err = expectPermission(certPath, 0o644)
		if err != nil {
			return fmt.Errorf("cannot verify X509 certificate permission: %w", err)
		}
		err = expectPermission(keyPath, 0o600)
		if err != nil {
			return fmt.Errorf("cannot verify X509 certificate permission: %w", err)
		}

		// We will process both the X509 certificate and the private key together now.
		skipNext = true

		keypair := x509KeyPair{Order: order}
		keypair.Private, err = privateKeyFromPEM(keyPath)
		if err != nil {
			return fmt.Errorf("cannot load private key %q: %w", keyPath, err)
		}
		keypair.Certificate, err = x509CertFromPEM(certPath)
		if err != nil {
			return fmt.Errorf("cannot load x509 certificate %q: %w", keyPath, err)
		}
		c.x509KeyPairs = append(c.x509KeyPairs, keypair)
	}
	return nil
}

// generateX509ECP256Keypair generates an elliptic curve P256 private key
// and an X509 self-signed public certificate (signed with the private key).
// The x509 certificate includes both the public key and the signature,
// which allows the certificate signature to be verified with the public key
// inside the certificate. This allows a client side copy of the certificate
// to be used as certificate authority for future TLS sessions with the server.
func generateX509ECP256Keypair(notBefore time.Time, notAfter time.Time) (*x509KeyPair, error) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	// Serial just random for now.
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
	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return nil, err
	}
	certificate, err := x509.ParseCertificate(derBytes)
	if err != nil {
		return nil, err
	}
	return &x509KeyPair{Private: privateKey, Certificate: certificate}, nil
}

func writeX509Keypair(keypair *x509KeyPair, tlsDir string) error {
	keyPath := filepath.Join(tlsDir, fmt.Sprintf("%03d-key.pem", keypair.Order))
	keyOut, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	keyPEM, err := privateKeyToPEM(keypair.Private)
	if err != nil {
		return err
	}
	if _, err = keyOut.Write(keyPEM); err != nil {
		return err
	}
	if err := keyOut.Close(); err != nil {
		return err
	}
	certPath := filepath.Join(tlsDir, fmt.Sprintf("%03d-cert.pem", keypair.Order))
	certOut, err := os.OpenFile(certPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	certPEM, err := x509CertToPEM(keypair.Certificate)
	if err != nil {
		return err
	}
	if _, err = certOut.Write(certPEM); err != nil {
		return err
	}
	if err := certOut.Close(); err != nil {
		return err
	}
	return nil
}

// x509CertFromPEM loads an X509 certificate to a PEM encoded file.
func x509CertFromPEM(path string) (*x509.Certificate, error) {
	pemData, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(pemData)
	if block == nil {
		return nil, fmt.Errorf("empty PEM block")
	}
	return x509.ParseCertificate(block.Bytes)
}

// x509CertToPEM converts an X509 certificate to a PEM encoded byte slice.
func x509CertToPEM(cert *x509.Certificate) ([]byte, error) {
	pemBlock := &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: cert.Raw,
	}
	var pemBuffer bytes.Buffer
	if err := pem.Encode(&pemBuffer, pemBlock); err != nil {
		return nil, fmt.Errorf("cannot convert certificate to PEM: %w", err)
	}
	return pemBuffer.Bytes(), nil
}

// privateKeyFromPEM loads a private key from a PEM encoded file.
func privateKeyFromPEM(path string) (any, error) {
	pemData, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(pemData)
	if block == nil {
		return nil, fmt.Errorf("empty PEM block")
	}
	return x509.ParsePKCS8PrivateKey(block.Bytes)
}

// privateKeyToPEM converts a private key to a PEM encoded byte slice.
func privateKeyToPEM(key any) ([]byte, error) {
	privateBytes, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, err
	}
	pemBlock := &pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: privateBytes,
	}
	var pemBuffer bytes.Buffer
	if err := pem.Encode(&pemBuffer, pemBlock); err != nil {
		return nil, fmt.Errorf("cannot convert key to PEM: %w", err)
	}
	return pemBuffer.Bytes(), nil
}

// isCertExpired checks if a X509 certificate is expired.
func isCertExpired(cert *x509.Certificate) bool {
	now := time.Now()
	if now.Before(cert.NotBefore) {
		return true
	}
	if now.After(cert.NotAfter) {
		return true
	}
	return false
}

// expectPermission return an error if the specified directory or file
// path is not matching the expected permission.
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
