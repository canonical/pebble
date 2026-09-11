// Copyright 2011 The Go Authors. All rights reserved.
//
// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions are
// met:
//
//    * Redistributions of source code must retain the above copyright
// notice, this list of conditions and the following disclaimer.
//    * Redistributions in binary form must reproduce the above
// copyright notice, this list of conditions and the following disclaimer
// in the documentation and/or other materials provided with the
// distribution.
//    * Neither the name of Google LLC nor the names of its
// contributors may be used to endorse or promote products derived from
// this software without specific prior written permission.
//
// THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
// "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
// LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
// A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
// OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
// SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
// LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
// DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
// THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
// (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
// OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.

//go:build linux

package truststate

import (
	"bytes"
	"crypto/x509"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	// certFileEnv is the environment variable which identifies where to
	// locate the SSL certificate file. If set this overrides the system
	// default. This matches the standard library (see
	// crypto/x509/root_unix.go).
	certFileEnv = "SSL_CERT_FILE"

	// certDirEnv is the environment variable which identifies which
	// directory to check for SSL certificate files. If set this overrides
	// the system default. It is a colon separated list of directories. This
	// matches the standard library (see crypto/x509/root_unix.go).
	certDirEnv = "SSL_CERT_DIR"
)

// certFiles lists possible certificate bundle files; only the first one
// found is read. This is a copy of the equivalent (unexported) list in
// crypto/x509/root_linux.go.
var certFiles = []string{
	"/etc/ssl/certs/ca-certificates.crt",                // Debian/Ubuntu/Gentoo etc.
	"/etc/pki/tls/certs/ca-bundle.crt",                  // Fedora/RHEL 6
	"/etc/ssl/ca-bundle.pem",                            // OpenSUSE
	"/etc/pki/tls/cacert.pem",                           // OpenELEC
	"/etc/pki/ca-trust/extracted/pem/tls-ca-bundle.pem", // CentOS/RHEL 7
	"/etc/ssl/cert.pem",                                 // Alpine Linux
}

// certDirectories lists possible directories with certificate files; all
// files in all of these directories are read. This is a copy of the
// equivalent (unexported) list in crypto/x509/root_linux.go (excluding the
// Android-specific entries, which aren't relevant to Pebble).
var certDirectories = []string{
	"/etc/ssl/certs",     // SLES10/SLES11, https://golang.org/issue/12139
	"/etc/pki/tls/certs", // Fedora/RHEL
}

// loadSystemCABundle locates and reads the same CA certificate data
// that the standard library's x509.SystemCertPool uses on Linux (see
// crypto/x509/root_unix.go's loadSystemRoots), so that a CA bundle file can
// be maintained on disk in addition to the in-memory pool. It returns nil,
// nil if no CA certificate data could be found.
func loadSystemCABundle() (*x509.CertPool, []byte) {
	pool := x509.NewCertPool()
	var bundle bytes.Buffer
	found := false

	files := certFiles
	if f := os.Getenv(certFileEnv); f != "" {
		files = []string{f}
	}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		pool.AppendCertsFromPEM(data)
		writePEMPart(&bundle, data)
		found = true
		break
	}

	dirs := certDirectories
	if d := os.Getenv(certDirEnv); d != "" {
		// OpenSSL and BoringSSL both use ":" as the SSL_CERT_DIR separator.
		dirs = strings.Split(d, ":")
	}
	for _, dir := range dirs {
		entries, err := readUniqueDirectoryEntries(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
			if err != nil {
				continue
			}
			pool.AppendCertsFromPEM(data)
			writePEMPart(&bundle, data)
			found = true
		}
	}

	if !found {
		return nil, nil
	}
	return pool, bundle.Bytes()
}

// readUniqueDirectoryEntries is like os.ReadDir but omits symlinks that
// point within the directory. This is a copy of the equivalent (unexported)
// function in crypto/x509/root_unix.go.
func readUniqueDirectoryEntries(dir string) ([]fs.DirEntry, error) {
	files, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	uniq := files[:0]
	for _, f := range files {
		if !isSameDirSymlink(f, dir) {
			uniq = append(uniq, f)
		}
	}
	return uniq, nil
}

// isSameDirSymlink reports whether f in dir is a symlink with a target not
// containing a slash. This is a copy of the equivalent (unexported) function
// in crypto/x509/root_unix.go.
func isSameDirSymlink(f fs.DirEntry, dir string) bool {
	if f.Type()&fs.ModeSymlink == 0 {
		return false
	}
	target, err := os.Readlink(filepath.Join(dir, f.Name()))
	return err == nil && !strings.Contains(target, "/")
}
