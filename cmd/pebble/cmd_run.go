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

package main

import (
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/jessevdk/go-flags"

	"github.com/canonical/pebble/client"
	"github.com/canonical/pebble/cmd"
	"github.com/canonical/pebble/internal/daemon"
	"github.com/canonical/pebble/internal/logger"
	"github.com/canonical/pebble/internal/systemd"
)

var shortRunHelp = "Run the pebble environment"
var longRunHelp = `
The run command starts pebble and runs the configured environment.
`

type cmdRun struct {
	clientMixin

	CreateDirs bool `long:"create-dirs"`
	Hold       bool `long:"hold"`
	Verbose    bool `short:"v" long:"verbose"`
}

func init() {
	addCommand("run", shortRunHelp, longRunHelp, func() flags.Commander { return &cmdRun{} },
		map[string]string{
			"create-dirs": "Create pebble directory on startup if it doesn't exist",
			"hold":        "Do not start default services automatically",
			"verbose":     "Log all output from services to stdout",
		}, nil)
}

func (rcmd *cmdRun) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	sigs := make(chan os.Signal, 2)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	if err := runDaemon(rcmd, sigs); err != nil {
		if err == daemon.ErrRestartSocket {
			// No "error: " prefix as this isn't an error.
			fmt.Fprintf(os.Stdout, "%v\n", err)
			// This exit code must be in system'd SuccessExitStatus.
			panic(&exitStatus{42})
		}
		fmt.Fprintf(os.Stderr, "cannot run pebble: %v\n", err)
		panic(&exitStatus{1})
	}

	return nil
}

func runWatchdog(d *daemon.Daemon) (*time.Ticker, error) {
	if os.Getenv("WATCHDOG_USEC") == "" {
		// Not running under systemd.
		return nil, nil
	}
	usec, err := strconv.ParseFloat(os.Getenv("WATCHDOG_USEC"), 10)
	if usec == 0 || err != nil {
		return nil, fmt.Errorf("cannot parse WATCHDOG_USEC: %q", os.Getenv("WATCHDOG_USEC"))
	}
	dur := time.Duration(usec/2) * time.Microsecond
	logger.Debugf("Setting up sd_notify() watchdog timer every %s", dur)
	wt := time.NewTicker(dur)

	go func() {
		for {
			select {
			case <-wt.C:
				// TODO: poke the API here and only report WATCHDOG=1 if it
				//       replies with valid data.
				systemd.SdNotify("WATCHDOG=1")
			case <-d.Dying():
				return
			}
		}
	}()

	return wt, nil
}

var checkRunningConditionsRetryDelay = 300 * time.Second

func sanityCheck() error {
	// Nothing interesting to check for now. See snapd's sanity package for examples.
	return nil
}

func runDaemon(rcmd *cmdRun, ch chan os.Signal) error {
	t0 := time.Now().Truncate(time.Millisecond)

	pebbleDir, socketPath := getEnvPaths()
	if rcmd.CreateDirs {
		err := os.MkdirAll(pebbleDir, 0755)
		if err != nil {
			return err
		}
	}
	dopts := daemon.Options{
		Dir:        pebbleDir,
		SocketPath: socketPath,
	}
	if rcmd.Verbose {
		dopts.ServiceOutput = os.Stdout
	}

	d, err := daemon.New(&dopts)
	if err != nil {
		return err
	}
	if err := d.Init(); err != nil {
		return err
	}

	// Run sanity check now, if anything goes wrong with the
	// check we go into "degraded" mode where we always report
	// the given error to any client.
	var checkTicker <-chan time.Time
	var tic *time.Ticker
	if err := sanityCheck(); err != nil {
		degradedErr := fmt.Errorf("system is not healthy: %s", err)
		logger.Noticef("%s", degradedErr)
		d.SetDegradedMode(degradedErr)
		tic = time.NewTicker(checkRunningConditionsRetryDelay)
		checkTicker = tic.C
	}

	d.Version = cmd.Version
	d.Start()

	watchdog, err := runWatchdog(d)
	if err != nil {
		return fmt.Errorf("cannot run software watchdog: %v", err)
	}
	if watchdog != nil {
		defer watchdog.Stop()
	}

	logger.Debugf("activation done in %v", time.Now().Truncate(time.Millisecond).Sub(t0))

	if !rcmd.Hold {
		servopts := client.ServiceOptions{}
		changeID, err := rcmd.client.AutoStart(&servopts)
		if err != nil {
			logger.Noticef("Cannot start default services: %v", err)
		} else {
			logger.Noticef("Started default services with change %s.", changeID)
		}
	}

out:
	for {
		select {
		case sig := <-ch:
			logger.Noticef("Exiting on %s signal.\n", sig)
			break out
		case <-d.Dying():
			// something called Stop()
			logger.Noticef("Server exiting!")
			break out
		case <-checkTicker:
			if err := sanityCheck(); err == nil {
				d.SetDegradedMode(nil)
				tic.Stop()
			}
		}
	}

	// Close our own self-connection, otherwise it prevents fast and clean termination.
	rcmd.client.CloseIdleConnections()

	return d.Stop(ch)
}
