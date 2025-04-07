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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha512"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base32"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

var (
	// systemTime can be faked during testing.
	systemTime = time.Now

	// certificateValidityPeriod determines how long a newly generated
	// TLS keypair is valid for.
	certificateValidityPeriod = (10 * 365 * 24 * time.Hour)
)

type TLSManager struct {
	// tlsDir is the location of the PEM keypair files.
	tlsDir         string
	tlsKeyPairLock sync.Mutex
	tlsKeyPairs    []*TLSKeyPair
}

// NewManager creates a new TLS manager and loads existing keypairs from the
// tlsDir directory.
//
// The TLS manager will generate keypairs on demand, so no external keypair
// generation or lifecycle management of existing keypairs will be required.
//
// However, is possible to pre-seed the tlsDir with TLSv1.3 supported keypairs.
// The following steps provide an example of how to create a TLSv1.3 supported
// self-signed TLS keypair (the private key must be encoded in PKCS8 format):
//
// ~/> openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:prime256v1 -out ec-p256.pem
// ~/> openssl req -new -x509 -key ec-p256.pem -out ec-p256-cert.pem -days 365
//
// The name of the file must match the SHA512/384 hash of the raw public certificate
// encoded in base32 (without padding), and contain both the private and certificate
// blocks in a single PEM file:
//
// ~/> FINGERPRINT=$(openssl x509 -in ec-p256-cert.pem -outform DER | openssl dgst -sha384 -binary | base32 -w0 | tr -d '=')
// ~/> cat ec-p256.pem > ${FINGERPRINT}.pem
// ~/> cat ec-p256-cert.pem >> ${FINGERPRINT}.pem
//
// The file permission must be 0o600:
//
// ~/> chmod 600 ${FINGERPRINT}.pem
//
// The file ${FINGERPRINT}.pem should be copied into tlsDir before the manager is started.
func NewManager(tlsDir string) (*TLSManager, error) {
	manager := &TLSManager{tlsDir: tlsDir}
	if err := manager.loadTLSKeyPairs(); err != nil {
		return nil, err
	}
	return manager, nil
}

// TLSKeyPair returns the currently preferred keypair. This method may generate
// a suitable TLS keypair if none exists as the time of the call. The
// generated keypair will be installed (persisted) in the TLS directory.
func (m *TLSManager) TLSKeyPair() (*TLSKeyPair, error) {
	m.tlsKeyPairLock.Lock()
	defer m.tlsKeyPairLock.Unlock()

	var selectedKeypair *TLSKeyPair
	// Try to find an existing valid certificate.
	for i, keypair := range m.tlsKeyPairs {
		if !keypair.isExpired() && m.isTLSKeyPairSupported(keypair) {
			selectedKeypair = m.tlsKeyPairs[i]
			break
		}
	}
	// Generate a keypair if no one exists.
	if selectedKeypair == nil {
		var err error
		notBefore := systemTime()
		notAfter := notBefore.Add(certificateValidityPeriod)
		selectedKeypair, err = m.addNewTLSKeyPair(notBefore, notAfter)
		if err != nil {
			return nil, err
		}
	}
	// Return the TLS keypair.
	return selectedKeypair, nil
}

// TLSKeyPairs returns the slice of currently valid and supported tlsKeyPairs.
func (m *TLSManager) TLSKeyPairs() []*TLSKeyPair {
	m.tlsKeyPairLock.Lock()
	defer m.tlsKeyPairLock.Unlock()

	var activeTLSKeyPairs []*TLSKeyPair
	for _, keypair := range m.tlsKeyPairs {
		if !keypair.isExpired() && m.isTLSKeyPairSupported(keypair) {
			activeTLSKeyPairs = append(activeTLSKeyPairs, keypair)
		}
	}
	return activeTLSKeyPairs
}

// Ensure implements StateManager.Ensure.
func (m *TLSManager) Ensure() error {
	return nil
}

// loadTLSKeypairs loads all the keypairs from the TLS directory. This
// method can be called again to reload the directory during run-time
// as long as the tlsKeyPairLock is locked is held. The keypairs are ordered
// by the certificate start date, then end date.
func (m *TLSManager) loadTLSKeyPairs() error {
	var keypairs []*TLSKeyPair
	err := m.withPEMFile(func(path string, fingerprint string) error {
		keypair := &TLSKeyPair{}
		err := keypair.fromPEMFile(path)
		if err != nil {
			return fmt.Errorf("cannot load keypair from PEM file %q: %w", path, err)
		}
		keypair.addFingerprint()
		if keypair.fingerprint != fingerprint {
			return fmt.Errorf("cannot match fingerprint with filename: %q vs. %q", keypair.fingerprint, fingerprint)
		}

		keypairs = append(keypairs, keypair)
		return nil
	})
	if err != nil {
		return err
	}
	// Sort by valid starting date, end date and fingerprint.
	slices.SortFunc(keypairs, func(a, b *TLSKeyPair) int {
		return a.Compare(b)
	})
	m.tlsKeyPairs = keypairs
	return nil
}

// filenameRegexp matches PEM files based on an SHA512/384 hash of the
// x509 raw certificate, encoded in base32 (77 digits), with an ".pem" suffix.
var filenameRegexp = regexp.MustCompile(`^[ABCDEFGHIJKLMNOPQRSTUVWXYZ234567]{77}\.pem$`)

// withPEMFile inspects the TLS directory, locates all the PEM files and
// call the 'do' function on each valid file. A non-existant or empty TLS
// directory is valid, but a TLS directory or PEM file with invalid
// permissions will result in an error. Files in the TLS directory not
// matching the naming convention will be ignored.
func (m *TLSManager) withPEMFile(do func(path string, fingerprint string) error) error {
	_, err := os.Stat(m.tlsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
	}
	err = expectPermission(m.tlsDir, 0o700)
	if err != nil {
		return fmt.Errorf("cannot verify TLS directory permissions: %w", err)
	}
	entries, err := os.ReadDir(m.tlsDir)
	if err != nil {
		return fmt.Errorf("cannot read TLS directory %q: %w", m.tlsDir, err)
	}

	for _, entry := range entries {
		match := filenameRegexp.FindStringSubmatch(entry.Name())
		if match == nil {
			continue
		}

		pemPath := filepath.Join(m.tlsDir, entry.Name())
		err = expectPermission(pemPath, 0o600)
		if err != nil {
			return fmt.Errorf("cannot verify PEM permission for %q: %w", pemPath, err)
		}
		fileNameSplit := strings.Split(entry.Name(), ".pem")
		// Do the required action.
		err = do(pemPath, fileNameSplit[0])
		if err != nil {
			return err
		}
	}
	return nil
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

// isTLSKeyPairSupported returns true if the TLS keypair is supported.
func (m *TLSManager) isTLSKeyPairSupported(keypair *TLSKeyPair) bool {
	// We currently support NIST P-256, P-384, and P-521 elliptic curve
	// based private keys used for certificate self signing and during
	// the TLSv1.3 handshake.
	keySupported := false
	switch privateKey := keypair.privateKey.(type) {
	case *ecdsa.PrivateKey:
		if privateKey.Curve.Params().BitSize >= 256 {
			keySupported = true
		}
	}
	// The signing algorithm will match the EC private key length.
	certSigningSupported := false
	switch keypair.Certificate.SignatureAlgorithm {
	case x509.ECDSAWithSHA256, x509.ECDSAWithSHA384, x509.ECDSAWithSHA512:
		certSigningSupported = true
	}
	// We only do ECDSA for now, as is evident from the above.
	certPublicKeySupported := keypair.Certificate.PublicKeyAlgorithm == x509.ECDSA

	return keySupported && certSigningSupported && certPublicKeySupported
}

// addNewTLSKeyPair creates a new supported TLS keypair starting on the supplied
// date, and expiring after the period defined in certificateValidityPeriod.
// The keypair is first persisted to disk, after which it is added to the
// managers keypair slice. The keypair slice is sorted before the method returns.
func (m *TLSManager) addNewTLSKeyPair(notBefore time.Time, notAfter time.Time) (*TLSKeyPair, error) {
	// Generate
	keypair, err := m.genECP256Keypair(notBefore, notAfter)
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

// genECP256Keypair generates a new keypair based on ECDSA NIST P-256
// private key and X509 public certificate. The certificate is signed with
// the same private key (self-signed), which means that the public key
// in the certificate can be used to verify the signature.
func (m *TLSManager) genECP256Keypair(notBefore time.Time, notAfter time.Time) (*TLSKeyPair, error) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
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

// persistTLSKeyPair persists the TLS keypair in a single PEM file. The file name
// is the certificate fingerprint string, with a .pem suffix. See the
// addFingerprint method for information on how the fingerprint is generated.
func (m *TLSManager) persistTLSKeyPair(keypair *TLSKeyPair) (err error) {
	// If the TLS directory does not yet exist, create it.
	_, err = os.Stat(m.tlsDir)
	if os.IsNotExist(err) {
		// Create the leaf directory node with 0700 permissions.
		err = os.Mkdir(m.tlsDir, 0700)
	}
	if err != nil {
		return fmt.Errorf("cannot create directory leaf of path %v: %w", m.tlsDir, err)
	}

	pemPath := filepath.Join(m.tlsDir, fmt.Sprintf("%v.pem", keypair.Fingerprint()))
	pemFile, err := os.OpenFile(pemPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, pemFile.Sync())
		err = errors.Join(err, pemFile.Close())
	}()

	pemData, err := keypair.toPEMBytes()
	if err != nil {
		return err
	}
	if _, err = pemFile.Write(pemData); err != nil {
		return err
	}
	return nil
}

// TLSKeyPair represents a TLS key pair (see tls.LoadX509KeyPair), which
// includes both the x509 certificate and the private key (unexported). This
// type can directly be converted to a tls.Certificate for configuring a TLS
// listener, or exported as a PEM encoded x509 certificate for easy transport
// over the daemon API.
type TLSKeyPair struct {
	privateKey  any
	Certificate *x509.Certificate
	fingerprint string
}

// Compare compares two keypairs and returns an int as used by sorting
// functions.
func (t TLSKeyPair) Compare(other *TLSKeyPair) int {
	// Start time first.
	notBeforeSort := t.Certificate.NotBefore.Compare(other.Certificate.NotBefore)
	if notBeforeSort != 0 {
		return notBeforeSort
	}
	// End time next.
	notAfterSort := t.Certificate.NotAfter.Compare(other.Certificate.NotAfter)
	if notAfterSort != 0 {
		return notAfterSort
	}
	// Fingerprint last.
	return strings.Compare(t.Fingerprint(), other.Fingerprint())
}

// Fingerprint returns an unpadded base32 encoded SHA512/384 hash of the raw
// x509 certificate.
func (t TLSKeyPair) Fingerprint() string {
	return t.fingerprint
}

// TLSCertificate returns a TLS certificate suitable for configuring a
// TLS based listener.
func (t TLSKeyPair) TLSCertificate() (*tls.Certificate, error) {
	pemCert, err := t.PEMCertificate()
	if err != nil {
		return nil, err
	}
	pemKey, err := t.pemKey()
	if err != nil {
		return nil, err
	}
	tlsCert, err := tls.X509KeyPair(pemCert, pemKey)
	if err != nil {
		return nil, err
	}
	return &tlsCert, err
}

// PEMCertificate converts the X509 certificate to a PEM encoded byte slice,
// useful for sending to daemon API clients.
func (t TLSKeyPair) PEMCertificate() ([]byte, error) {
	pemBlock := &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: t.Certificate.Raw,
	}
	var pemBuffer bytes.Buffer
	if err := pem.Encode(&pemBuffer, pemBlock); err != nil {
		return nil, fmt.Errorf("cannot convert certificate to PEM: %w", err)
	}
	return pemBuffer.Bytes(), nil
}

// pemKey returns a PKCS8 encoded private key in PEM format.
func (t TLSKeyPair) pemKey() ([]byte, error) {
	privateBytes, err := x509.MarshalPKCS8PrivateKey(t.privateKey)
	if err != nil {
		return nil, err
	}
	pemPrivateBlock := &pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: privateBytes,
	}
	var pemBuffer bytes.Buffer
	if err := pem.Encode(&pemBuffer, pemPrivateBlock); err != nil {
		return nil, fmt.Errorf("cannot convert key to PEM: %w", err)
	}
	return pemBuffer.Bytes(), nil
}

// toPEMBytes converts the private key and x509 certificate into a byte slice
// that contains two PEM blocks, ready for secure storage on disk.
func (t TLSKeyPair) toPEMBytes() ([]byte, error) {
	privateBytes, err := x509.MarshalPKCS8PrivateKey(t.privateKey)
	if err != nil {
		return nil, err
	}
	pemPrivateBlock := &pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: privateBytes,
	}
	pemCertificateBlock := &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: t.Certificate.Raw,
	}
	var pemBuffer bytes.Buffer
	if err := pem.Encode(&pemBuffer, pemPrivateBlock); err != nil {
		return nil, fmt.Errorf("cannot convert key to PEM: %w", err)
	}
	if err := pem.Encode(&pemBuffer, pemCertificateBlock); err != nil {
		return nil, fmt.Errorf("cannot convert x509 certificate to PEM: %w", err)
	}
	return pemBuffer.Bytes(), nil
}

// fromPEMFile loads a PKCS8 private key and x509 certificate
// from a PEM formatted file.
func (t *TLSKeyPair) fromPEMFile(path string) error {
	pemData, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	for {
		var block *pem.Block
		block, pemData = pem.Decode(pemData)
		if block == nil {
			break
		}
		switch block.Type {
		case "PRIVATE KEY":
			t.privateKey, err = x509.ParsePKCS8PrivateKey(block.Bytes)
			if err != nil {
				return err
			}
		case "CERTIFICATE":
			t.Certificate, err = x509.ParseCertificate(block.Bytes)
			if err != nil {
				return err
			}
		}
	}
	if t.privateKey == nil || t.Certificate == nil {
		return fmt.Errorf("PEM file does not contain the expected keypair")
	}
	return nil
}

// isExpired returns true if the X509 certificate has expired.
func (t TLSKeyPair) isExpired() bool {
	now := systemTime()
	if now.Before(t.Certificate.NotBefore) || now.After(t.Certificate.NotAfter) {
		return true
	}
	return false
}

// addFingerprint adds the base32 string encoded fingerprint of the public certificate
// so we can access it later as a cached value. No fingerprint will be added if
// this method is called before a certificate was added.
func (t *TLSKeyPair) addFingerprint() {
	if t.Certificate != nil {
		hashBytes := sha512.Sum384(t.Certificate.Raw)
		t.fingerprint = base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(hashBytes[:])
	}
}
