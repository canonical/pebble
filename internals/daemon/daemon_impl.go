//go:build !fips

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

package daemon

import (
	"crypto/tls"
	"fmt"

	"github.com/canonical/pebble/internals/logger"
)

// initHTTPSListener creates HTTPS listener in the default build.
func (d *Daemon) initHTTPSListener() error {
	if d.options.HTTPSAddress != "" {
		tlsConf := d.overlord.TLSManager().ListenConfig()
		listener, err := tls.Listen("tcp", d.options.HTTPSAddress, tlsConf)
		if err != nil {
			return fmt.Errorf("cannot TLS listen on %q: %v", d.options.HTTPSAddress, err)
		}
		d.httpsListener = listener
		logger.Noticef("HTTPS API server listening on %q.", d.options.HTTPSAddress)
	}
	return nil
}
