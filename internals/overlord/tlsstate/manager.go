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
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/canonical/pebble/cmd"
)

var (
	// timeNow can be faked during testing.
	timeNow = time.Now

	// idCertValidity sets the x509 identity key validity period. The
	// certificate is managed by the TLS manager as it is not directly
	// supported by a crypto.Signer interface, as its an implementation
	// detail related to TLS (not a signing key). Expiry of this
	// certificate does not change the identity, but changes the client
	// pinned identity certificate, forcing the client to re-pair with
	// the server. The period is currently just over 10 years.
	idCertValidity = 10 * 366 * 24 * time.Hour

	// tlsCertValidity defines how long the in-memory TLS keypairs are
	// valid before a new keypair is generated. This allows for
	// TLS session resumption during this period, and keypair re-use
	// from concurrent requests. The manager restart will always result
	// in the creation of a new TLS keypair.
	tlsCertValidity = time.Hour

	// tlsCertRenewWindow defines how long before the certificate
	// expiry we should actually rotate, in order to avoid a race between
	// the expiry and the session handshake completing in time. This
	// means the last practical time a new TLS session can be started
	// for a TLS certificate is tlsCertRenewWindow period before its
	// NotAfter timestamp. The rotation happens immediately after this
	// point in time.
	tlsCertRenewWindow = 60 * time.Second

	// idCertFile is the public x509 certificate, which holds the
	// identity public key, and self-signed with the identity key.
	idCertFile = "identity.pem"
)

// IDSigner includes a crypto.Signer, and expects the provided signer
// to know how to generate an identity fingerprint. We leave this to
// the identity signer to ensure a consistent representation of the
// fingerprint, instead of relying on every consumer to generate a
// compliant fingerprint.
type IDSigner interface {
	crypto.Signer
	Fingerprint() string
}

// ConfigureCertificateFunc is called during certificate creation to allow
// customisation of the certificate fields. The cert parameter is the
// certificate being created, and parentCopy is a deep copy of the signing
// (parent) certificate, or nil when the certificate is self-signed.
// Returning an error aborts certificate creation.
type ConfigureCertificateFunc func(cert *x509.Certificate, parentCopy *x509.Certificate) error

// Options holds the configuration for creating a TLSManager.
type Options struct {
	// TLSDir is the directory used to persist identity certificate files. It
	// must be a directory of which only the leaf element, at most, does not
	// exist. The leaf directory will be created with 0o700 permissions.
	TLSDir string
	// Signer is a private key representing the identity of a Pebble
	// instance (machine, container or device), which implements the
	// tlsstate.Signer interface (allowing it to sign digests).
	Signer IDSigner
	// ConfigureIDCertificate, if non-nil, is called after the default identity
	// certificate fields are set and may override any of them.
	ConfigureIDCertificate ConfigureCertificateFunc
	// ConfigureTLSCertificate, if non-nil, is called after the default TLS
	// certificate fields are set and may override any of them.
	ConfigureTLSCertificate ConfigureCertificateFunc
}

type TLSManager struct {
	// tlsDir is the location of the PEM keypair files.
	tlsDir string
	mu     sync.RWMutex
	// The active in-memory TLS certificate (keypair).
	tlsCert *tls.Certificate
	// The identity certificate loaded from disk.
	idCert *x509.Certificate
	// The identity key used for signing TLS certificates.
	signer IDSigner

	// Optional caller-supplied functions applied after the defaults when
	// creating the identity and TLS certificates respectively.
	configureIDCert  ConfigureCertificateFunc
	configureTLSCert ConfigureCertificateFunc
}

// NewManager creates a new TLS keypair manager using the provided options.
// opts.TLSDir must be a directory of which only the leaf element, at most,
// does not exist; the leaf directory will be created with 0o700 permissions.
// opts.Signer represents the identity key of the machine, container or device
// and will be used to sign TLS keypairs and the identity certificate.
func NewManager(opts *Options) *TLSManager {
	return &TLSManager{
		tlsDir:           opts.TLSDir,
		signer:           opts.Signer,
		configureIDCert:  opts.ConfigureIDCertificate,
		configureTLSCert: opts.ConfigureTLSCertificate,
	}
}

func generateSerialNumber() (*big.Int, error) {
	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return nil, err
	}

	return serialNumber, nil
}

func withDefaultIDTemplate(signer IDSigner) ConfigureCertificateFunc {
	return func(c *x509.Certificate, parentCopy *x509.Certificate) (err error) {
		serialNumber, err := generateSerialNumber()
		if err != nil {
			return err
		}
		c.SerialNumber = serialNumber
		c.Subject = defaultCertSubject(signer.Fingerprint())
		c.NotBefore = timeNow()
		c.NotAfter = c.NotBefore.Add(idCertValidity)
		c.KeyUsage = x509.KeyUsageCertSign | x509.KeyUsageCRLSign
		c.ExtKeyUsage = []x509.ExtKeyUsage{}
		c.BasicConstraintsValid = true
		c.IsCA = true
		// We can only sign leaf certificates with this.
		c.MaxPathLen = 0
		c.MaxPathLenZero = true
		return nil
	}
}

func withDefaultTLSTemplate(signer IDSigner) ConfigureCertificateFunc {
	return func(c *x509.Certificate, parentCopy *x509.Certificate) (err error) {
		c.SerialNumber, err = generateSerialNumber()
		if err != nil {
			return err
		}
		c.Subject = defaultCertSubject(signer.Fingerprint())
		c.NotBefore = timeNow()
		c.NotAfter = c.NotBefore.Add(tlsCertValidity)
		c.KeyUsage = x509.KeyUsageDigitalSignature
		c.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
		c.BasicConstraintsValid = true
		return nil
	}
}

// ListenConfig provides a complete TLS default configuration, which includes the
// GetCertificate method for obtaining a valid TLS certificate.
func (m *TLSManager) ListenConfig() *tls.Config {
	tlsConf := &tls.Config{
		// Enable server support for HTTP1.1 and HTTP2 over TLS. The server
		// will only pick HTTP2 if the client indicated the same support in
		// the client hello message. The Pebble client will not pick HTTP2
		// because it requires integrated websockets to work, which does
		// not support HTTP2 (it does not yet implement RFC8441). However,
		// external clients, such as curl, will be able to switch to HTTP2
		// for non-websocket dependant API calls.
		NextProtos:       []string{"h2", "http/1.1"},
		MinVersion:       tls.VersionTLS13,
		GetCertificate:   m.GetCertificate,
		ClientAuth:       tls.RequestClientCert,
		VerifyConnection: m.VerifyClientCertificate,
	}
	return tlsConf
}

func (m *TLSManager) VerifyClientCertificate(state tls.ConnectionState) error {
	numCerts := len(state.PeerCertificates)
	if numCerts != 1 {
		return fmt.Errorf("expected one client certificate, received %d", numCerts)
	}
	return nil
}

// GetCertificate returns an identity signed TLS certificate. The certificate chain includes
// both the TLS leaf certificate, as well as the root CA identity certificate. If
// either the identity or TLS certificate nears expiry, this functions creates new
// certificates on demand. Note that even if the identity certificate is re-created, this
// does not mean that the identity key changed (the key itself has no expiry).
func (m *TLSManager) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	// Fast path: concurrent sessions while the ID and TLS certificate is valid.
	m.mu.RLock()
	tlsCert := m.tlsCert
	idCert := m.idCert
	m.mu.RUnlock()
	if idCert != nil && tlsCert != nil && isCertActive(tlsCert.Leaf) {
		return tlsCert, nil
	}

	// Slow path: generate a new in-memory identity signed TLS certificate.
	//
	// If we got here then it means we need to generate a new in-memory TLS
	// keypair, and potentially an identity certificate (only the first time
	// or when the identity key changed).
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.createDir(); err != nil {
		return nil, fmt.Errorf("cannot create TLS directory: %w", err)
	}
	if err := m.ensureIDCert(); err != nil {
		return nil, fmt.Errorf("cannot get identity certificate: %w", err)
	}
	if err := m.createTLSCert(); err != nil {
		return nil, fmt.Errorf("cannot create TLS certificate: %w", err)
	}
	return m.tlsCert, nil
}

// Ensure implements StateManager.Ensure.
func (m *TLSManager) Ensure() error {
	return nil
}

func (m *TLSManager) createTLSCert() error {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}

	// Create a deep copy of the parent (identity) certificate.
	parent, err := x509.ParseCertificate(m.idCert.Raw)
	if err != nil {
		return err
	}

	// Apply defaults then the optional caller-supplied configuration.
	template := x509.Certificate{}
	if err := withDefaultTLSTemplate(m.signer)(&template, parent); err != nil {
		return err
	}
	if m.configureTLSCert != nil {
		if err := m.configureTLSCert(&template, parent); err != nil {
			return err
		}
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
// the leaf element of the TLS directory if it does not yet exist.
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

// ensureIDCert verifies that the identity certificate exists, and that the
// certificate matches the supplied identity key signer. If the identity key
// has changed, or the certificate has expired, the identity certificate is
// replaced with a new identity certificate signed with the identity key.
func (m *TLSManager) ensureIDCert() error {
	idPath := filepath.Join(m.tlsDir, idCertFile)

	if m.idCert == nil {
		// No identity certificate loaded yet.
		exists, err := pathExists(idPath)
		if err != nil {
			return err
		}
		if exists {
			// Load if the certificate exists on disk.
			m.idCert, err = loadIDCert(idPath)
			if err != nil {
				return err
			}
		}
	}

	// Can we use the loaded identity certificate?
	if m.idCert != nil {
		if isCertDerived(m.idCert, m.signer) {
			// The existing identity certificate still matches
			// our identity private key.
			//
			// NOTE: The identity certificate can expire, and we do
			// not currently re-generate identity certificates to
			// limit insecure time based attacks. Future work will
			// add a safe identity certificate rotation scheme.
			return nil
		}
	}

	// If we get here a new identity certificate must be created.
	var err error
	m.idCert, err = createIDCert(m.signer, m.configureIDCert)
	if err != nil {
		return err
	}
	err = saveIDCert(idPath, m.idCert)
	if err != nil {
		return err
	}
	return nil
}

// loadIDCert loads the x509 identity certificate, which contains
// the identity public key. We only allow a single certificate
// block in the PEM file.
func loadIDCert(path string) (*x509.Certificate, error) {
	err := expectPermission(path, 0o600)
	if err != nil {
		return nil, err
	}

	var cert *x509.Certificate
	pemData, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, rest := pem.Decode(pemData)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, fmt.Errorf("missing 'CERTIFICATE' block in %q", path)
	}
	if len(rest) != 0 {
		return nil, fmt.Errorf("unexpected bytes after 'CERTIFICATE' block in %q", path)
	}
	cert, err = x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, err
	}
	return cert, nil
}

// saveIDCert saves the x509 identity certificate, which contains
// the identity public key.
func saveIDCert(path string, cert *x509.Certificate) (err error) {
	certPEMBlock := &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: cert.Raw,
	}
	pemFile, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		// err here refers to the named error return.
		err = errors.Join(err, pemFile.Close())
	}()

	if err = pem.Encode(pemFile, certPEMBlock); err != nil {
		return err
	}
	if err = pemFile.Sync(); err != nil {
		return err
	}
	return nil
}

// createIDCert create a public x509 CA certificate, signed by the identity
// key. This certificate is included in the TLS certificate chain as the
// non-leaf certificate, allowing the client to pin this certificate during
// the client server trust exchange (pairing) procedure.
func createIDCert(signer IDSigner, configure ConfigureCertificateFunc) (*x509.Certificate, error) {
	// Apply defaults then the optional caller-supplied configuration.
	template := &x509.Certificate{}
	if err := withDefaultIDTemplate(signer)(template, nil); err != nil {
		return nil, err
	}
	if configure != nil {
		if err := configure(template, nil); err != nil {
			return nil, err
		}
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
	now := timeNow()
	// Note that we shorten the NotAfter timestamp by a tlsCertRenewWindow
	// duration to avoid a TLS handshake race against the expiry time.
	if now.Before(cert.NotBefore) || now.After(cert.NotAfter.Add(-tlsCertRenewWindow)) {
		return false
	}
	return true
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
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	return true, nil
}

// defaultCertSubject provides a default Subject Common Name for x509
// certificates. The ProgramName and fingerprint must not include
// non-ASCII characters, but this is not the case (see: idkey package).
func defaultCertSubject(fingerprint string) pkix.Name {
	// The common name must not exceed 64 bytes (RFC 5280).
	var cn strings.Builder
	// <= 55 bytes for the prefix (program name). The program name
	// will never have non-ASCII characters (r < 128).
	cn.WriteString(cmd.ProgramName[:min(len(cmd.ProgramName), 55)])
	// 1 byte for the separator.
	cn.WriteString("-")
	// 8 bytes for the identity fingerprint. The program name
	// will never have non-ASCII characters (r < 128).
	cn.WriteString(fingerprint[:min(len(fingerprint), 8)])

	subject := pkix.Name{
		CommonName: cn.String(),
	}
	return subject
}
