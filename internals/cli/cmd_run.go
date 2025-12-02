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

package cli

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/canonical/go-flags"

	"github.com/canonical/pebble/client"
	"github.com/canonical/pebble/cmd"
	"github.com/canonical/pebble/internals/daemon"
	"github.com/canonical/pebble/internals/logger"
	"github.com/canonical/pebble/internals/overlord"
	"github.com/canonical/pebble/internals/overlord/pairingstate"
	"github.com/canonical/pebble/internals/plan"
	"github.com/canonical/pebble/internals/reaper"
	"github.com/canonical/pebble/internals/systemd"
	"github.com/canonical/pebble/internals/workloads"
)

const cmdRunSummary = "Run the service manager environment"
const cmdRunDescription = `
The run command starts {{.DisplayName}} and runs the configured environment.

Additional arguments may be provided to the service command with the --args
option, which must be terminated with ";" unless there are no further program
options. These arguments are appended to the end of the service command, and
replace any default arguments defined in the service plan. For example:

{{.ProgramName}} run --args myservice --port 8080 \; --hold
`

type sharedRunEnterOpts struct {
	CreateDirs bool       `long:"create-dirs"`
	Hold       bool       `long:"hold"`
	HTTP       string     `long:"http"`
	HTTPS      string     `long:"https"`
	Verbose    bool       `short:"v" long:"verbose"`
	Args       [][]string `long:"args" terminator:";"`
	Identities string     `long:"identities"`
}

var sharedRunEnterArgsHelp = map[string]string{
	"--create-dirs": "Create {{.DisplayName}} directory on startup if it doesn't exist",
	"--hold":        "Do not start default services automatically",
	"--http":        `Start HTTP API listening on this address in "<address>:port" format (for example, ":4000", "192.0.2.0:4000", "[2001:db8::1]:4000")`,
	"--https":       `Start HTTPS API listening on this address in "<address>:port" format (for example, ":8443", "192.0.2.0:8443", "[2001:db8::1]:8443")`,
	"--verbose":     "Log all output from services to stdout (also PEBBLE_VERBOSE=1)",
	"--args":        "Provide additional arguments to a service",
	"--identities":  "Seed identities from file (like update-identities --replace)",
}

type cmdRun struct {
	client *client.Client

	socketPath string
	pebbleDir  string

	sharedRunEnterOpts
}

func init() {
	AddCommand(&CmdInfo{
		Name:        "run",
		Summary:     cmdRunSummary,
		Description: cmdRunDescription,
		ArgsHelp:    sharedRunEnterArgsHelp,
		New: func(opts *CmdOptions) flags.Commander {
			return &cmdRun{
				client:     opts.Client,
				socketPath: opts.SocketPath,
				pebbleDir:  opts.PebbleDir,
			}
		},
	})
}

func (rcmd *cmdRun) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	rcmd.run(nil)

	return nil
}

func (rcmd *cmdRun) run(ready chan<- func()) {
	sigs := make(chan os.Signal, 2)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	if err := runDaemon(rcmd, sigs, ready); err != nil {
		switch {
		case errors.Is(err, daemon.ErrRestartSocket):
			// No "error: " prefix as this isn't an error.
			fmt.Fprintf(os.Stdout, "%v\n", err)
			// This exit code must be in system'd SuccessExitStatus.
			panic(&exitStatus{42})
		case errors.Is(err, daemon.ErrRestartServiceFailure):
			// Daemon returns distinct code for service-failure shutdown.
			panic(&exitStatus{10})
		case errors.Is(err, daemon.ErrRestartCheckFailure):
			// Daemon returns distinct code for check-failure shutdown.
			panic(&exitStatus{11})
		}
		fmt.Fprintf(os.Stderr, "cannot run daemon: %v\n", err)
		panic(&exitStatus{1})
	}
}

func runWatchdog(d *daemon.Daemon) (*time.Ticker, error) {
	if os.Getenv("WATCHDOG_USEC") == "" {
		// Not running under systemd.
		return nil, nil
	}
	usec, err := strconv.ParseFloat(os.Getenv("WATCHDOG_USEC"), 64)
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

func runDaemon(rcmd *cmdRun, ch chan os.Signal, ready chan<- func()) error {
	err := reaper.Start()
	if err != nil {
		return fmt.Errorf("cannot start child process reaper: %w", err)
	}
	defer func() {
		err := reaper.Stop()
		if err != nil {
			logger.Noticef("Cannot stop child process reaper: %v", err)
		}
	}()

	t0 := time.Now().Truncate(time.Millisecond)

	if rcmd.CreateDirs {
		err := os.MkdirAll(rcmd.pebbleDir, 0o700)
		if err != nil {
			return err
		}
	}
	err = maybeCopyPebbleDir(rcmd.pebbleDir, getCopySource())
	if err != nil {
		return err
	}

	plan.RegisterSectionExtension(workloads.WorkloadsField, &workloads.WorkloadsSectionExtension{})
	plan.RegisterSectionExtension(pairingstate.PairingField, &pairingstate.SectionExtension{})

	idSigner, err := getIDSigner(rcmd.pebbleDir)
	if err != nil {
		return err
	}

	dopts := daemon.Options{
		Dir:          rcmd.pebbleDir,
		SocketPath:   rcmd.socketPath,
		IDSigner:     idSigner,
		HTTPAddress:  rcmd.HTTP,
		HTTPSAddress: rcmd.HTTPS,
	}
	if os.Getenv("PEBBLE_VERBOSE") == "1" || rcmd.Verbose {
		dopts.ServiceOutput = os.Stdout
	}
	if os.Getenv("PEBBLE_PERSIST") == "never" {
		dopts.Persist = overlord.PersistNever
	}

	d, err := daemon.New(&dopts)
	if err != nil {
		return err
	}
	if err := d.Init(); err != nil {
		return err
	}

	if rcmd.Args != nil {
		mappedArgs, err := convertArgs(rcmd.Args)
		if err != nil {
			return err
		}
		if err := d.SetServiceArgs(mappedArgs); err != nil {
			return err
		}
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
	if err := d.Start(); err != nil {
		return err
	}

	watchdog, err := runWatchdog(d)
	if err != nil {
		return fmt.Errorf("cannot run software watchdog: %v", err)
	}
	if watchdog != nil {
		defer watchdog.Stop()
	}

	logger.Debugf("activation done in %v", time.Now().Truncate(time.Millisecond).Sub(t0))

	if rcmd.Identities != "" {
		identities, err := readIdentities(rcmd.Identities)
		if err != nil {
			return fmt.Errorf("cannot read identities: %w", err)
		}
		err = rcmd.client.ReplaceIdentities(identities)
		if err != nil {
			return fmt.Errorf("cannot replace identities: %w", err)
		}
	}

	// The "stop" channel is used by the "enter" command to stop the daemon.
	var stop chan struct{}
	if ready != nil {
		stop = make(chan struct{}, 1)
	}
	notifyReady := func() {
		ready <- func() { close(stop) }
		close(ready)
	}

	if !rcmd.Hold {
		// Start the default services (those configured with startup: enabled).
		servopts := client.ServiceOptions{}
		changeID, err := rcmd.client.AutoStart(&servopts)
		if err != nil {
			logger.Noticef("Cannot start default services: %v", err)
		} else {
			// Wait for the default services to actually start and then notify
			// the ready channel (for the "enter" command).
			go func() {
				logger.Debugf("Waiting for default services to autostart with change %s.", changeID)
				_, err := rcmd.client.WaitChange(changeID, nil)
				if err != nil {
					logger.Noticef("Cannot wait for autostart change %s: %v", changeID, err)
				} else {
					logger.Noticef("Started default services with change %s.", changeID)
				}
				if ready != nil {
					notifyReady()
				}
			}()
		}
	} else if ready != nil {
		notifyReady()
	}

out:
	for {
		select {
		case sig := <-ch:
			logger.Noticef("Exiting on %s signal.\n", sig)
			break out
		case <-d.Dying():
			// something called Stop()
			logger.Noticef("Server exiting! Reason: %v", d.Err())
			break out
		case <-checkTicker:
			if err := sanityCheck(); err == nil {
				d.SetDegradedMode(nil)
				tic.Stop()
			}
		case <-stop:
			break out
		}
	}

	// Close the client idle connection to the server (self connection) before we
	// start with the HTTP/HTTPS shutdown process. This will speed up the server
	// shutdown, and allow the Pebble process to exit faster.
	rcmd.client.CloseIdleConnections()

	return d.Stop(ch)
}

// convert args from [][]string type to map[string][]string
// and check for empty or duplicated --args usage
func convertArgs(args [][]string) (map[string][]string, error) {
	mappedArgs := make(map[string][]string)

	for _, arg := range args {
		if len(arg) < 1 {
			return nil, fmt.Errorf("--args requires a service name")
		}
		name := arg[0]
		if _, ok := mappedArgs[name]; ok {
			return nil, fmt.Errorf("--args provided more than once for %q service", name)
		}
		mappedArgs[name] = arg[1:]
	}

	return mappedArgs, nil
}

func maybeCopyPebbleDir(destDir, srcDir string) error {
	if srcDir == "" {
		return nil
	}
	_, err := os.Stat(srcDir)
	if errors.Is(err, os.ErrNotExist) {
		// Skip missing source directory.
		return nil
	} else if err != nil {
		return err
	}
	entries, err := os.ReadDir(destDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	} else if len(entries) != 0 {
		// Skip non-empty dir.
		return nil
	}
	fsys := os.DirFS(srcDir)
	err = os.CopyFS(destDir, fsys)
	if err != nil {
		return fmt.Errorf("cannot copy %q to %q: %w", srcDir, destDir, err)
	}
	return nil
}
