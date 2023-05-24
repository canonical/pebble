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

package daemon

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"gopkg.in/tomb.v2"

	"github.com/gorilla/mux"

	"github.com/canonical/pebble/internal/logger"
	"github.com/canonical/pebble/internal/osutil"
	"github.com/canonical/pebble/internal/osutil/sys"
	"github.com/canonical/pebble/internal/overlord"
	"github.com/canonical/pebble/internal/overlord/checkstate"
	"github.com/canonical/pebble/internal/overlord/restart"
	"github.com/canonical/pebble/internal/overlord/servstate"
	"github.com/canonical/pebble/internal/overlord/standby"
	"github.com/canonical/pebble/internal/overlord/state"
	"github.com/canonical/pebble/internal/systemd"
)

var (
	ErrRestartSocket = fmt.Errorf("daemon stop requested to wait for socket activation")

	systemdSdNotify = systemd.SdNotify
	sysGetuid       = sys.Getuid
)

// Options holds the daemon setup required for the initialization of a new daemon.
type Options struct {
	// Dir is the pebble directory where all setup is found. Defaults to /var/lib/pebble/default.
	Dir string

	// SocketPath is an optional path for the unix socket used for the client
	// to communicate with the daemon. Defaults to a hidden (dotted) name inside
	// the pebble directory.
	SocketPath string

	// HTTPAddress is the address for the plain HTTP API server, for example
	// ":4000" to listen on any address, port 4000. If not set, the HTTP API
	// server is not started.
	HTTPAddress string

	// ServiceOuput is an optional io.Writer for the service log output, if set, all services
	// log output will be written to the writer.
	ServiceOutput io.Writer
}

// A Daemon listens for requests and routes them to the right command
type Daemon struct {
	Version             string
	StartTime           time.Time
	pebbleDir           string
	normalSocketPath    string
	untrustedSocketPath string
	httpAddress         string
	overlord            *overlord.Overlord
	state               *state.State
	generalListener     net.Listener
	untrustedListener   net.Listener
	httpListener        net.Listener
	connTracker         *connTracker
	serve               *http.Server
	tomb                tomb.Tomb
	router              *mux.Router
	standbyOpinions     *standby.StandbyOpinions

	// set to remember we need to restart the system
	restartSystem bool

	// set to remember that we need to exit the daemon in a way that
	// prevents systemd from restarting it
	restartSocket bool

	// degradedErr is set when the daemon is in degraded mode
	degradedErr error

	rebootIsMissing bool

	mu sync.Mutex
}

// XXX Placeholder for now.
type userState struct{}

// A ResponseFunc handles one of the individual verbs for a method
type ResponseFunc func(*Command, *http.Request, *userState) Response

// A Command routes a request to an individual per-verb ResponseFUnc
type Command struct {
	Path       string
	PathPrefix string
	//
	GET         ResponseFunc
	PUT         ResponseFunc
	POST        ResponseFunc
	DELETE      ResponseFunc
	GuestOK     bool
	UserOK      bool
	UntrustedOK bool
	AdminOnly   bool

	d *Daemon
}

type accessResult int

const (
	accessOK accessResult = iota
	accessUnauthorized
	accessForbidden
)

// canAccess checks the following properties:
//
// - if the user is `root` everything is allowed
// - if a user is logged in and the command doesn't have AdminOnly, everything is allowed
// - POST/PUT/DELETE all require the admin, or just login if not AdminOnly
//
// Otherwise for GET requests the following parameters are honored:
// - GuestOK: anyone can access GET
// - UserOK: any uid on the local system can access GET
// - AdminOnly: only the administrator can access this
// - UntrustedOK: can access this via the untrusted socket
func (c *Command) canAccess(r *http.Request, user *userState) accessResult {
	if c.AdminOnly && (c.UserOK || c.GuestOK || c.UntrustedOK) {
		logger.Panicf("internal error: command cannot have AdminOnly together with any *OK flag")
	}

	if user != nil && !c.AdminOnly {
		// Authenticated users do anything not requiring explicit admin.
		return accessOK
	}

	// isUser means we have a UID for the request
	isUser := false
	pid, uid, socket, err := ucrednetGet(r.RemoteAddr)
	if err == nil {
		isUser = true
	} else if err != errNoID {
		logger.Noticef("unexpected error when attempting to get UID: %s", err)
		return accessForbidden
	}

	isUntrusted := (socket == c.d.untrustedSocketPath)

	_ = pid
	_ = uid

	if isUntrusted {
		if c.UntrustedOK {
			return accessOK
		}
		return accessUnauthorized
	}

	// the !AdminOnly check is redundant, but belt-and-suspenders
	if r.Method == "GET" && !c.AdminOnly {
		// Guest and user access restricted to GET requests
		if c.GuestOK {
			return accessOK
		}

		if isUser && c.UserOK {
			return accessOK
		}
	}

	// Remaining admin checks rely on identifying peer uid
	if !isUser {
		return accessUnauthorized
	}

	if uid == 0 || sys.UserID(uid) == sysGetuid() {
		// Superuser and process owner can do anything.
		return accessOK
	}

	if c.AdminOnly {
		return accessUnauthorized
	}

	return accessUnauthorized
}

func userFromRequest(state interface{}, r *http.Request) (*userState, error) {
	return nil, nil
}

func (c *Command) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	st := c.d.state
	st.Lock()
	user, err := userFromRequest(st, r)
	if err != nil {
		statusForbidden("forbidden").ServeHTTP(w, r)
		return
	}
	st.Unlock()

	// check if we are in degradedMode
	if c.d.degradedErr != nil && r.Method != "GET" {
		statusInternalError(c.d.degradedErr.Error()).ServeHTTP(w, r)
		return
	}

	switch c.canAccess(r, user) {
	case accessOK:
		// nothing
	case accessUnauthorized:
		statusUnauthorized("access denied").ServeHTTP(w, r)
		return
	case accessForbidden:
		statusForbidden("forbidden").ServeHTTP(w, r)
		return
	}

	var rspf ResponseFunc
	var rsp = statusMethodNotAllowed("method %q not allowed", r.Method)

	switch r.Method {
	case "GET":
		rspf = c.GET
	case "PUT":
		rspf = c.PUT
	case "POST":
		rspf = c.POST
	case "DELETE":
		rspf = c.DELETE
	}

	if rspf != nil {
		rsp = rspf(c, r, user)
	}

	if rsp, ok := rsp.(*resp); ok {
		st.Lock()
		_, rst := restart.Pending(st)
		st.Unlock()
		switch rst {
		case restart.RestartSystem:
			rsp.transmitMaintenance(errorKindSystemRestart, "system is restarting")
		case restart.RestartDaemon:
			rsp.transmitMaintenance(errorKindDaemonRestart, "daemon is restarting")
		case restart.RestartSocket:
			rsp.transmitMaintenance(errorKindDaemonRestart, "daemon is stopping to wait for socket activation")
		}
		if rsp.Type != ResponseTypeError {
			st.Lock()
			count, stamp := st.WarningsSummary()
			st.Unlock()
			rsp.addWarningsToMeta(count, stamp)
		}
	}

	rsp.ServeHTTP(w, r)
}

type wrappedWriter struct {
	w http.ResponseWriter
	s int
}

func (w *wrappedWriter) Header() http.Header {
	return w.w.Header()
}

func (w *wrappedWriter) Write(bs []byte) (int, error) {
	return w.w.Write(bs)
}

func (w *wrappedWriter) WriteHeader(s int) {
	w.w.WriteHeader(s)
	w.s = s
}

func (w *wrappedWriter) Flush() {
	if f, ok := w.w.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack is needed for websockets to take over an HTTP connection.
func (w *wrappedWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.w.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("underlying writer does not implement Hijack")
	}
	return hijacker.Hijack()
}

func (w *wrappedWriter) status() int {
	if w.s == 0 {
		// If status was not explicitly written, HTTP 200 is implied.
		return http.StatusOK
	}
	return w.s
}

func logit(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ww := &wrappedWriter{w: w}
		t0 := time.Now()
		handler.ServeHTTP(ww, r)
		t := time.Now().Sub(t0)

		// Don't log GET /v1/changes/{change-id} as that's polled quickly by
		// clients when waiting for a change (e.g., service starting). Also
		// don't log GET /v1/system-info or GET /v1/health to avoid hits to
		// those filling logs with noise (Juju hits them every 5s for checking
		// health, for example).
		skipLog := r.Method == "GET" &&
			(strings.HasPrefix(r.URL.Path, "/v1/changes/") && strings.Count(r.URL.Path, "/") == 3 ||
				r.URL.Path == "/v1/system-info" ||
				r.URL.Path == "/v1/health")
		if !skipLog {
			if strings.HasSuffix(r.RemoteAddr, ";") {
				logger.Debugf("%s %s %s %s %d", r.RemoteAddr, r.Method, r.URL, t, ww.status())
				logger.Noticef("%s %s %s %d", r.Method, r.URL, t, ww.status())
			} else {
				logger.Noticef("%s %s %s %s %d", r.RemoteAddr, r.Method, r.URL, t, ww.status())
			}
		}
	})
}

// Init sets up the Daemon's internal workings.
// Don't call more than once.
func (d *Daemon) Init() error {
	listenerMap := make(map[string]net.Listener)

	if listener, err := getListener(d.normalSocketPath, listenerMap); err == nil {
		d.generalListener = &ucrednetListener{Listener: listener}
	} else {
		return fmt.Errorf("when trying to listen on %s: %v", d.normalSocketPath, err)
	}

	if listener, err := getListener(d.untrustedSocketPath, listenerMap); err == nil {
		// This listener may also be nil if that socket wasn't among
		// the listeners, so check it before using it.
		d.untrustedListener = &ucrednetListener{Listener: listener}
	} else {
		logger.Debugf("cannot get listener for %q: %v", d.untrustedSocketPath, err)
	}

	d.addRoutes()

	if d.httpAddress != "" {
		listener, err := net.Listen("tcp", d.httpAddress)
		if err != nil {
			return fmt.Errorf("cannot listen on %q: %v", d.httpAddress, err)
		}
		d.httpListener = listener
		logger.Noticef("HTTP API server listening on %q.", d.httpAddress)
	}

	logger.Noticef("Started daemon.")
	return nil
}

// SetDegradedMode puts the daemon into a degraded mode which will the
// error given in the "err" argument for commands that are not marked
// as readonlyOK.
//
// This is useful to report errors to the client when the daemon
// cannot work because e.g. a sanity check failed or the system is out
// of diskspace.
//
// When the system is fine again calling "DegradedMode(nil)" is enough
// to put the daemon into full operation again.
func (d *Daemon) SetDegradedMode(err error) {
	d.degradedErr = err
}

func (d *Daemon) addRoutes() {
	d.router = mux.NewRouter()

	for _, c := range api {
		c.d = d
		if c.PathPrefix == "" {
			d.router.Handle(c.Path, c).Name(c.Path)
		} else {
			d.router.PathPrefix(c.PathPrefix).Handler(c).Name(c.PathPrefix)
		}
	}

	// also maybe add a /favicon.ico handler...

	d.router.NotFoundHandler = statusNotFound("invalid API endpoint requested")
}

type connTracker struct {
	mu    sync.Mutex
	conns map[net.Conn]struct{}
}

func (ct *connTracker) CanStandby() bool {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	return len(ct.conns) == 0
}

func (ct *connTracker) trackConn(conn net.Conn, state http.ConnState) {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	// we ignore hijacked connections, if we do things with websockets
	// we'll need custom shutdown handling for them
	if state == http.StateNew || state == http.StateActive {
		ct.conns[conn] = struct{}{}
	} else {
		delete(ct.conns, conn)
	}
}

func (d *Daemon) CanStandby() bool {
	return systemd.SocketAvailable()
}

func (d *Daemon) initStandbyHandling() {
	d.standbyOpinions = standby.New(d.state)
	d.standbyOpinions.AddOpinion(d)
	d.standbyOpinions.AddOpinion(d.connTracker)
	d.standbyOpinions.AddOpinion(d.overlord)
	d.standbyOpinions.Start()
}

func (d *Daemon) Start() {
	if d.rebootIsMissing {
		// we need to schedule and wait for a system restart
		d.tomb.Kill(nil)
		// avoid systemd killing us again while we wait
		systemdSdNotify("READY=1")
		return
	}
	if d.overlord == nil {
		panic("internal error: no Overlord")
	}

	d.StartTime = time.Now()

	d.connTracker = &connTracker{conns: make(map[net.Conn]struct{})}
	d.serve = &http.Server{
		Handler:   logit(d.router),
		ConnState: d.connTracker.trackConn,
	}

	d.initStandbyHandling()

	d.overlord.Loop()

	d.tomb.Go(func() error {
		if d.untrustedListener != nil {
			d.tomb.Go(func() error {
				if err := d.serve.Serve(d.untrustedListener); err != http.ErrServerClosed && d.tomb.Err() == tomb.ErrStillAlive {
					return err
				}
				return nil
			})
		}
		if err := d.serve.Serve(d.generalListener); err != http.ErrServerClosed && d.tomb.Err() == tomb.ErrStillAlive {
			return err
		}
		return nil
	})

	if d.httpListener != nil {
		// Start additional HTTP API (currently only GuestOK endpoints are
		// available because the HTTP API has no authentication right now).
		d.tomb.Go(func() error {
			err := d.serve.Serve(d.httpListener)
			if err != http.ErrServerClosed && d.tomb.Err() == tomb.ErrStillAlive {
				return err
			}
			return nil
		})
	}

	// notify systemd that we are ready
	systemdSdNotify("READY=1")
}

// HandleRestart implements overlord.RestartBehavior.
func (d *Daemon) HandleRestart(t restart.RestartType) {
	// die when asked to restart (systemd should get us back up!) etc
	switch t {
	case restart.RestartDaemon:
	case restart.RestartSystem:
		// try to schedule a fallback slow reboot already here
		// in case we get stuck shutting down
		if err := reboot(rebootWaitTimeout); err != nil {
			logger.Noticef("%s", err)
		}

		d.mu.Lock()
		defer d.mu.Unlock()
		// remember we need to restart the system
		d.restartSystem = true
	case restart.RestartSocket:
		d.mu.Lock()
		defer d.mu.Unlock()
		d.restartSocket = true
	default:
		logger.Noticef("internal error: restart handler called with unknown restart type: %v", t)
	}
	d.tomb.Kill(nil)
}

var (
	rebootNoticeWait       = 3 * time.Second
	rebootWaitTimeout      = 10 * time.Minute
	rebootRetryWaitTimeout = 5 * time.Minute
	rebootMaxTentatives    = 3
)

var shutdownTimeout = time.Second

// Stop shuts down the Daemon.
func (d *Daemon) Stop(sigCh chan<- os.Signal) error {
	if d.rebootIsMissing {
		// we need to schedule/wait for a system restart again
		return d.doReboot(sigCh, rebootRetryWaitTimeout)
	}
	if d.overlord == nil {
		return fmt.Errorf("internal error: no Overlord")
	}

	// Stop all running services. Must do this before overlord.Stop, as it
	// creates a change and waits for the change, and overlord.Stop calls
	// StateEngine.Stop, which locks, so Ensure would result in a deadlock.
	err := d.stopRunningServices()
	if err != nil {
		// This isn't fatal for exiting the daemon, so log and continue.
		logger.Noticef("Cannot stop running services: %v", err)
	}

	d.tomb.Kill(nil)

	d.mu.Lock()
	restartSystem := d.restartSystem
	restartSocket := d.restartSocket
	d.mu.Unlock()

	d.generalListener.Close()
	d.standbyOpinions.Stop()

	if d.untrustedListener != nil {
		d.untrustedListener.Close()
	}

	if d.httpListener != nil {
		d.httpListener.Close()
	}

	if restartSystem {
		// give time to polling clients to notice restart
		time.Sleep(rebootNoticeWait)
	}

	// We're using the background context here because the tomb's
	// context will likely already have been cancelled when we are
	// called.
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	d.tomb.Kill(d.serve.Shutdown(ctx))
	cancel()

	if !restartSystem {
		// tell systemd that we are stopping
		systemdSdNotify("STOPPING=1")
	}

	if restartSocket {
		// At this point we processed all open requests (and
		// stopped accepting new requests) - before going into
		// socket activated mode we need to check if any of
		// those open requests resulted in something that
		// prevents us from going into socket activation mode.
		//
		// If this is the case we do a "normal" pebble restart
		// to process the new changes.
		if !d.standbyOpinions.CanStandby() {
			d.restartSocket = false
		}
	}
	d.overlord.Stop()

	err = d.tomb.Wait()
	if err != nil {
		// do not stop the shutdown even if the tomb errors
		// because we already scheduled a slow shutdown and
		// exiting here will just restart pebble (via systemd)
		// which will lead to confusing results.
		if restartSystem {
			logger.Noticef("WARNING: cannot stop daemon: %v", err)
		} else {
			return err
		}
	}

	if restartSystem {
		return d.doReboot(sigCh, rebootWaitTimeout)
	}

	if d.restartSocket {
		return ErrRestartSocket
	}

	return nil
}

// stopRunningServices stops all running services, waiting for a short time
// for them all to stop.
func (d *Daemon) stopRunningServices() error {
	taskSet, err := servstate.StopRunning(d.state, d.overlord.ServiceManager())
	if err != nil {
		return err
	}
	if taskSet == nil {
		logger.Debugf("No services to stop.")
		return nil
	}

	// One change to stop them all.
	logger.Noticef("Stopping all running services.")
	st := d.state
	st.Lock()
	chg := st.NewChange("stop", "Stop all running services")
	chg.AddAll(taskSet)
	st.EnsureBefore(0) // start operation right away
	st.Unlock()

	// Wait for a limited amount of time for them to stop.
	select {
	case <-chg.Ready():
		logger.Debugf("All services stopped.")
	case <-time.After(d.overlord.ServiceManager().StopTimeout()):
		return errors.New("timeout stopping running services")
	}
	return nil
}

func (d *Daemon) rebootDelay() (time.Duration, error) {
	d.state.Lock()
	defer d.state.Unlock()
	now := time.Now()
	// see whether a reboot had already been scheduled
	var rebootAt time.Time
	err := d.state.Get("daemon-system-restart-at", &rebootAt)
	if err != nil && err != state.ErrNoState {
		return 0, err
	}
	rebootDelay := 1 * time.Minute
	if err == nil {
		rebootDelay = rebootAt.Sub(now)
	} else {
		ovr := os.Getenv("PEBBLE_REBOOT_DELAY") // for tests
		if ovr != "" {
			d, err := time.ParseDuration(ovr)
			if err == nil {
				rebootDelay = d
			}
		}
		rebootAt = now.Add(rebootDelay)
		d.state.Set("daemon-system-restart-at", rebootAt)
	}
	return rebootDelay, nil
}

func (d *Daemon) doReboot(sigCh chan<- os.Signal, waitTimeout time.Duration) error {
	rebootDelay, err := d.rebootDelay()
	if err != nil {
		return err
	}
	// ask for shutdown and wait for it to happen.
	// if we exit, pebble will be restarted by systemd
	if err := reboot(rebootDelay); err != nil {
		return err
	}
	// wait for reboot to happen
	logger.Noticef("Waiting for system reboot")
	if sigCh != nil {
		signal.Stop(sigCh)
		if len(sigCh) > 0 {
			// a signal arrived in between
			return nil
		}
		close(sigCh)
	}
	time.Sleep(waitTimeout)
	return fmt.Errorf("expected reboot did not happen")
}

var shutdownMsg = "reboot scheduled to update the system"

func rebootImpl(rebootDelay time.Duration) error {
	if rebootDelay < 0 {
		rebootDelay = 0
	}
	mins := int64(rebootDelay / time.Minute)
	cmd := exec.Command("shutdown", "-r", fmt.Sprintf("+%d", mins), shutdownMsg)
	if out, err := cmd.CombinedOutput(); err != nil {
		return osutil.OutputErr(out, err)
	}
	return nil
}

var reboot = rebootImpl

func (d *Daemon) Dying() <-chan struct{} {
	return d.tomb.Dying()
}

func clearReboot(st *state.State) {
	// FIXME See notes in the state package. This logic should be
	// centralized in the overlord which is the orchestrator. Right
	// now we have the daemon, the overlord, and even the state
	// itself all knowing about such details.
	st.Set("daemon-system-restart-at", nil)
	st.Set("daemon-system-restart-tentative", nil)
}

// RebootIsFine implements part of overlord.RestartBehavior.
func (d *Daemon) RebootIsFine(st *state.State) error {
	clearReboot(st)
	return nil
}

var errExpectedReboot = errors.New("expected reboot did not happen")

// RebootIsMissing implements part of overlord.RestartBehavior.
func (d *Daemon) RebootIsMissing(st *state.State) error {
	var nTentative int
	err := st.Get("daemon-system-restart-tentative", &nTentative)
	if err != nil && err != state.ErrNoState {
		return err
	}
	nTentative++
	if nTentative > rebootMaxTentatives {
		// giving up, proceed normally, some in-progress refresh
		// might get rolled back!!
		restart.ClearReboot(st)
		clearReboot(st)
		logger.Noticef("pebble was restarted while a system restart was expected, pebble retried to schedule and waited again for a system restart %d times and is giving up", rebootMaxTentatives)
		return nil
	}
	st.Set("daemon-system-restart-tentative", nTentative)
	d.state = st
	logger.Noticef("pebble was restarted while a system restart was expected, pebble will try to schedule and wait for a system restart again (tenative %d/%d)", nTentative, rebootMaxTentatives)
	return errExpectedReboot
}

// SetServiceArgs sets the provided service arguments to their respective services,
// by passing the arguments to the service manager responsible under daemon overlord.
func (d *Daemon) SetServiceArgs(serviceArgs map[string][]string) error {
	return d.overlord.ServiceManager().SetServiceArgs(serviceArgs)
}

func New(opts *Options) (*Daemon, error) {
	d := &Daemon{
		pebbleDir:           opts.Dir,
		normalSocketPath:    opts.SocketPath,
		untrustedSocketPath: opts.SocketPath + ".untrusted",
		httpAddress:         opts.HTTPAddress,
	}

	ovld, err := overlord.New(opts.Dir, d, opts.ServiceOutput)
	if err == errExpectedReboot {
		// we proceed without overlord until we reach Stop
		// where we will schedule and wait again for a system restart.
		// ATM we cannot do that in New because we need to satisfy
		// systemd notify mechanisms.
		d.rebootIsMissing = true
		return d, nil
	}
	if err != nil {
		return nil, err
	}
	d.overlord = ovld
	d.state = ovld.State()
	return d, nil
}

// GetListener tries to get a listener for the given socket path from
// the listener map, and if it fails it tries to set it up directly.
func getListener(socketPath string, listenerMap map[string]net.Listener) (net.Listener, error) {
	if listener, ok := listenerMap[socketPath]; ok {
		return listener, nil
	}

	if c, err := net.Dial("unix", socketPath); err == nil {
		c.Close()
		return nil, fmt.Errorf("socket %q already in use", socketPath)
	}

	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	address, err := net.ResolveUnixAddr("unix", socketPath)
	if err != nil {
		return nil, err
	}

	runtime.LockOSThread()
	oldmask := syscall.Umask(0111)
	listener, err := net.ListenUnix("unix", address)
	syscall.Umask(oldmask)
	runtime.UnlockOSThread()
	if err != nil {
		return nil, err
	}

	logger.Debugf("socket %q was not activated; listening", socketPath)

	return listener, nil
}

var getChecks = func(o *overlord.Overlord) ([]*checkstate.CheckInfo, error) {
	return o.CheckManager().Checks()
}
