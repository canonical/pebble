// Copyright (c) 2022 Canonical Ltd
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

package main

import (
	"fmt"
	"os"
	"syscall"
	"time"

	"github.com/jessevdk/go-flags"

	"github.com/canonical/pebble/client"
	. "github.com/canonical/pebble/cmd"
	"github.com/canonical/pebble/internal/boot"
	"github.com/canonical/pebble/internal/daemon"
	"github.com/canonical/pebble/internal/logger"
	"github.com/canonical/pebble/internal/plan"
)

type cmdBootFirmware struct {
	clientMixin

	Force   bool `long:"force"`
	Verbose bool `short:"v" long:"verbose"`
}

var bootFirmwareDescs = map[string]string{
	"force":   `Skip all checks`,
	"verbose": `Log all output from services to stdout`,
}

var shortBootFirmwareHelp = `Bootstrap a system with Pebble running as PID 1`

var longBootFirmwareHelp = `
The boot-firmware command performs checks on the running system, prepares the
environment to get a working system and starts the Pebble daemon.
`

func (cmd *cmdBootFirmware) Execute(args []string) error {
	if len(args) > 1 {
		return ErrExtraArgs
	}

	if !cmd.Force {
		if err := boot.CheckBootstrap(); err != nil {
			return err
		}
	}

	if err := boot.Bootstrap(); err != nil {
		return err
	}

	t0 := time.Now().Truncate(time.Millisecond)

	pebbleDir, socketPath := getEnvPaths()
	if err := os.MkdirAll(pebbleDir, 0755); err != nil {
		return err
	}
	if _, err := plan.ReadDir(pebbleDir); err != nil {
		return err
	}

	dopts := daemon.Options{
		Dir:        pebbleDir,
		SocketPath: socketPath,
	}
	if cmd.Verbose {
		dopts.ServiceOutput = os.Stdout
	}

	d, err := daemon.New(&dopts)
	if err != nil {
		return err
	}
	if err := d.Init(); err != nil {
		return err
	}

	d.Version = Version
	d.Start()

	logger.Debugf("activation done in %v", time.Now().Truncate(time.Millisecond).Sub(t0))

	servopts := client.ServiceOptions{}
	changeID, err := cmd.client.AutoStart(&servopts)
	if err != nil {
		logger.Noticef("Cannot start default services: %v", err)
	} else {
		logger.Noticef("Started default services with change %s.", changeID)
	}

out:
	for {
		select {
		case <-d.Dying():
			logger.Noticef("Server exiting!")
			break out
		}
	}

	cmd.client.CloseIdleConnections()
	if err := d.Stop(nil); err != nil {
		return err
	}

	if err := syscall.Reboot(syscall.LINUX_REBOOT_CMD_RESTART); err != nil {
		return fmt.Errorf("cannot reboot: %w", err)
	}
	return nil
}

func init() {
	addCommand("boot-firmware", shortBootFirmwareHelp, longBootFirmwareHelp, func() flags.Commander { return &cmdBootFirmware{} }, bootFirmwareDescs, nil)
}
