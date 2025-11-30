//go:build fips

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
	"fmt"
)

// initHTTPSListener blocks HTTPS listener creation in FIPS builds.
func (d *Daemon) initHTTPSListener() error {
	if d.options.HTTPSAddress != "" {
		return fmt.Errorf("HTTPS server is not supported in FIPS builds")
	}
	return nil
}
