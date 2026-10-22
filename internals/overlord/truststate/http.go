// Copyright (c) 2026 Canonical Ltd
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

package truststate

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"sync"

	"github.com/canonical/pebble/internals/logger"
)

// Provider provides access to resolved trust contexts, as implemented by
// TrustManager.
type Provider interface {
	TrustContext(name string) (*TrustContext, error)
}

// HTTPTransport maintains an HTTP transport that validates server
// certificates against a named trust context. The transport is only rebuilt
// when the resolved trust context changes, so connections are reused between
// requests.
//
// If the trust context is explicitly named but cannot be resolved,
// RoundTripper fails rather than falling back to the system CA pool. If no
// trust context is named, the "default" trust context is used, falling back
// to the base transport (with a notice logged) if it cannot be resolved.
type HTTPTransport struct {
	// Provider resolves trust contexts. If nil, only an unnamed trust
	// context can be used, and the base transport is always returned.
	Provider Provider
	// Name is the name of the trust context, or "" for "default".
	Name string
	// Base is the transport used for the system CA pool, and cloned for
	// other trust contexts. If nil, http.DefaultTransport is used.
	Base http.RoundTripper

	mu        sync.Mutex
	version   string
	transport *http.Transport
}

// RoundTripper returns the transport to use for the next request.
func (t *HTTPTransport) RoundTripper() (http.RoundTripper, error) {
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}

	trustContext, err := t.resolve()
	if err != nil {
		if t.Name != "" {
			return nil, err
		}
		logger.Noticef("Cannot resolve default trust context, using system CA pool: %v", err)
		return base, nil
	}
	if trustContext == nil {
		return base, nil
	}
	defer trustContext.Close()
	if trustContext.IsSystemCA() {
		return base, nil
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	version := trustContext.Version()
	if t.transport != nil && t.version == version {
		return t.transport, nil
	}
	pool, err := trustContext.CAPool()
	if err != nil {
		return nil, fmt.Errorf("cannot get CA pool for trust context %q: %w", t.displayName(), err)
	}
	baseTransport, ok := base.(*http.Transport)
	if !ok || baseTransport == nil {
		baseTransport = http.DefaultTransport.(*http.Transport)
	}
	transport := baseTransport.Clone()
	if transport.TLSClientConfig != nil {
		transport.TLSClientConfig = transport.TLSClientConfig.Clone()
	} else {
		transport.TLSClientConfig = &tls.Config{}
	}
	transport.TLSClientConfig.RootCAs = pool
	if t.transport != nil {
		t.transport.CloseIdleConnections()
	}
	t.transport = transport
	t.version = version
	return transport, nil
}

// CloseIdleConnections closes any idle connections held by the transport
// built for the trust context.
func (t *HTTPTransport) CloseIdleConnections() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.transport != nil {
		t.transport.CloseIdleConnections()
	}
}

// resolve returns the trust context, or nil if there is no provider and no
// trust context is named.
func (t *HTTPTransport) resolve() (*TrustContext, error) {
	if t.Provider == nil {
		if t.Name == "" {
			return nil, nil
		}
		return nil, fmt.Errorf("cannot resolve trust context %q: no trust manager", t.Name)
	}
	trustContext, err := t.Provider.TrustContext(t.Name)
	if err != nil {
		return nil, fmt.Errorf("cannot resolve trust context %q: %w", t.displayName(), err)
	}
	return trustContext, nil
}

func (t *HTTPTransport) displayName() string {
	if t.Name == "" {
		return DefaultTrustContext
	}
	return t.Name
}
