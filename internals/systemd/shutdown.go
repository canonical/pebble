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

package systemd

import (
	"fmt"
	"os/exec"
	"time"

	"github.com/canonical/pebble/internals/osutil"
)

type shutdown struct{}

var Shutdown = &shutdown{}

// Reboot the system after a specified duration of time, optionally
// displaying a wall message.
func (s shutdown) Reboot(delay time.Duration, msg string) error {
	if delay < 0 {
		delay = 0
	}
	mins := int64(delay / time.Minute)
	cmd := exec.Command("shutdown", "-r", fmt.Sprintf("+%d", mins), msg)
	if out, err := cmd.CombinedOutput(); err != nil {
		return osutil.OutputErr(out, err)
	}
	return nil
}
