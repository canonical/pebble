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
	"io"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/canonical/go-flags"

	"github.com/canonical/pebble/client"
	"github.com/canonical/pebble/cmd"
	"github.com/canonical/pebble/internals/daemon"
	"github.com/canonical/pebble/internals/logger"
	"github.com/canonical/pebble/internals/systemd"
)

const cmdRunSummary = "Run the service manager environment"
const cmdRunDescription = `
The run command starts {{.DisplayName}} and runs the configured environment.

Additional arguments may be provided to the service command with the --args option, which
must be terminated with ";" unless there are no further program options.  These arguments
are appended to the end of the service command, and replace any default arguments defined
in the service plan. For example:

{{.ProgramName}} run --args myservice --port 8080 \; --hold
`

type sharedRunEnterOpts struct {
	CreateDirs bool       `long:"create-dirs"`
	Hold       bool       `long:"hold"`
	HTTP       string     `long:"http"`
	Verbose    bool       `short:"v" long:"verbose"`
	Args       [][]string `long:"args" terminator:";"`
}

var sharedRunEnterArgsHelp = map[string]string{
	"--create-dirs": "Create {{.DisplayName}} directory on startup if it doesn't exist",
	"--hold":        "Do not start default services automatically",
	"--http":        `Start HTTP API listening on this address (e.g., ":4000")`,
	"--verbose":     "Log all output from services to stdout",
	"--args":        `Provide additional arguments to a service`,
}

type cmdRun struct {
	client *client.Client

	sharedRunEnterOpts
}

func init() {
	AddCommand(&CmdInfo{
		Name:        "run",
		Summary:     cmdRunSummary,
		Description: cmdRunDescription,
		ArgsHelp:    sharedRunEnterArgsHelp,
		New: func(opts *CmdOptions) flags.Commander {
			return &cmdRun{client: opts.Client}
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

func runDaemon(rcmd *cmdRun, ch chan os.Signal, ready chan<- func()) error {
	t0 := time.Now().Truncate(time.Millisecond)

	pebbleDir, socketPath := getEnvPaths()
	if rcmd.CreateDirs {
		err := os.MkdirAll(pebbleDir, 0755)
		if err != nil {
			return err
		}
	}
	err := maybeCopyPebbleDir(getCopySource(), pebbleDir)
	if err != nil {
		return err
	}

	dopts := daemon.Options{
		Dir:        pebbleDir,
		SocketPath: socketPath,
	}
	if rcmd.Verbose {
		dopts.ServiceOutput = os.Stdout
	}
	dopts.HTTPAddress = rcmd.HTTP

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

	if !rcmd.Hold {
		servopts := client.ServiceOptions{}
		changeID, err := rcmd.client.AutoStart(&servopts)
		if err != nil {
			logger.Noticef("Cannot start default services: %v", err)
		} else {
			logger.Noticef("Started default services with change %s.", changeID)
		}
	}

	var stop chan struct{}
	if ready != nil {
		stop = make(chan struct{}, 1)
		ready <- func() { close(stop) }
		close(ready)
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
		case <-stop:
			break out
		}
	}

	// Close the client idle connection to the server (self connection) before we
	// start with the HTTP shutdown process. This will speed up the server shutdown,
	// and allow the Pebble process to exit faster.
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

func maybeCopyPebbleDir(srcDir, destDir string) error {
	if srcDir == "" {
		return nil
	}
	dirEnts, err := os.ReadDir(destDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	} else if len(dirEnts) != 0 {
		// Skip non-empty dir.
		return nil
	}
	fsys := os.DirFS(srcDir)
	// TODO: replace with os.CopyFS
	err = _go_os_CopyFS(destDir, fsys)
	if err != nil {
		return fmt.Errorf("cannot copy %q to %q: %w", srcDir, destDir, err)
	}
	return nil
}

// Implementation from https://go-review.googlesource.com/c/go/+/558995 until accepted into go.
// Copyright (c) 2009 The Go Authors. All rights reserved.
//
// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions are
// met:
//
//   - Redistributions of source code must retain the above copyright
//
// notice, this list of conditions and the following disclaimer.
//   - Redistributions in binary form must reproduce the above
//
// copyright notice, this list of conditions and the following disclaimer
// in the documentation and/or other materials provided with the
// distribution.
//   - Neither the name of Google Inc. nor the names of its
//
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

// _go_os_CopyFS copies the file system fsys into the directory dir,
// creating dir if necessary.
//
// Newly created directories and files have their default modes
// where any bits from the file in fsys that are not part of the
// standard read, write, and execute permissions will be zeroed
// out, and standard read and write permissions are set for owner,
// group, and others while retaining any existing execute bits from
// the file in fsys.
//
// Symbolic links in fsys are not supported, a *PathError with Err set
// to ErrInvalid is returned on symlink.
//
// Copying stops at and returns the first error encountered.
func _go_os_CopyFS(dir string, fsys fs.FS) error {
	return fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		newPath := filepath.Join(dir, path)
		if d.IsDir() {
			return os.MkdirAll(newPath, 0777)
		}

		// TODO(panjf2000): handle symlinks with the help of fs.ReadLinkFS
		// 		once https://go.dev/issue/49580 is done.
		//		we also need safefilepath.IsLocal from https://go.dev/cl/564295.
		if !d.Type().IsRegular() {
			return &fs.PathError{Op: "CopyFS", Path: path, Err: fs.ErrInvalid}
		}

		r, err := fsys.Open(path)
		if err != nil {
			return err
		}
		defer r.Close()
		info, err := r.Stat()
		if err != nil {
			return err
		}
		w, err := os.OpenFile(newPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0666|info.Mode()&0777)
		if err != nil {
			return err
		}

		if _, err := io.Copy(w, r); err != nil {
			w.Close()
			return &fs.PathError{Op: "Copy", Path: newPath, Err: err}
		}
		return w.Close()
	})
}
