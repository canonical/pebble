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
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"gopkg.in/check.v1"

	// XXX Delete import above and make this file like the other ones.
	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internal/osutil"
	"github.com/canonical/pebble/internal/overlord/patch"
	"github.com/canonical/pebble/internal/overlord/standby"
	"github.com/canonical/pebble/internal/overlord/state"
	"github.com/canonical/pebble/internal/systemd"
	"github.com/canonical/pebble/internal/testutil"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { check.TestingT(t) }

type daemonSuite struct {
	pebbleDir       string
	socketPath      string
	statePath       string
	authorized      bool
	err             error
	notified        []string
	restoreBackends func()
}

var _ = check.Suite(&daemonSuite{})

func (s *daemonSuite) SetUpTest(c *check.C) {
	s.pebbleDir = c.MkDir()
	s.statePath = filepath.Join(s.pebbleDir, ".pebble.state")
	systemdSdNotify = func(notif string) error {
		s.notified = append(s.notified, notif)
		return nil
	}
}

func (s *daemonSuite) TearDownTest(c *check.C) {
	systemdSdNotify = systemd.SdNotify
	s.notified = nil
	s.authorized = false
	s.err = nil
}

func (s *daemonSuite) newDaemon(c *check.C) *Daemon {
	d, err := New(&Options{Dir: s.pebbleDir, SocketPath: s.socketPath})
	c.Assert(err, check.IsNil)
	d.addRoutes()
	return d
}

// a Response suitable for testing
type fakeHandler struct {
	cmd        *Command
	lastMethod string
}

func (h *fakeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.lastMethod = r.Method
}

func (s *daemonSuite) TestDefaultPaths(c *C) {
	c.Assert(defaultPebbleDir, Equals, "/var/lib/pebble/default")

	originalDefaultDir := defaultPebbleDir
	defer func() {
		defaultPebbleDir = originalDefaultDir
	}()

	// newDaemon will use the empty directory, which makes the daemon
	// use the default as established by the global defaultPebbleDir.
	s.pebbleDir = ""
	defaultPebbleDir = c.MkDir()

	d := s.newDaemon(c)
	d.Init()
	d.Start()
	defer d.Stop(nil)

	info, err := os.Stat(filepath.Join(defaultPebbleDir, ".pebble.socket"))
	c.Assert(err, IsNil)
	c.Assert(info.Mode(), Equals, os.ModeSocket|0666)

	info, err = os.Stat(filepath.Join(defaultPebbleDir, ".pebble.socket.untrusted"))
	c.Assert(err, IsNil)
	c.Assert(info.Mode(), Equals, os.ModeSocket|0666)
}

func (s *daemonSuite) TestExplicitPaths(c *C) {
	s.socketPath = filepath.Join(c.MkDir(), "custom.socket")

	d := s.newDaemon(c)
	d.Init()
	d.Start()
	defer d.Stop(nil)

	info, err := os.Stat(s.socketPath)
	c.Assert(err, IsNil)
	c.Assert(info.Mode(), Equals, os.ModeSocket|0666)

	info, err = os.Stat(s.socketPath + ".untrusted")
	c.Assert(err, IsNil)
	c.Assert(info.Mode(), Equals, os.ModeSocket|0666)
}

func (s *daemonSuite) TestCommandMethodDispatch(c *check.C) {
	fakeUserAgent := "some-agent-talking-to-snapd/1.0"

	cmd := &Command{d: s.newDaemon(c)}
	handler := &fakeHandler{cmd: cmd}
	rf := func(innerCmd *Command, req *http.Request, user *userState) Response {
		c.Assert(cmd, check.Equals, innerCmd)
		return handler
	}
	cmd.GET = rf
	cmd.PUT = rf
	cmd.POST = rf
	cmd.DELETE = rf

	for _, method := range []string{"GET", "POST", "PUT", "DELETE"} {
		req, err := http.NewRequest(method, "", nil)
		req.Header.Add("User-Agent", fakeUserAgent)
		c.Assert(err, check.IsNil)

		rec := httptest.NewRecorder()
		cmd.ServeHTTP(rec, req)
		c.Check(rec.Code, check.Equals, 401, check.Commentf(method))

		rec = httptest.NewRecorder()
		req.RemoteAddr = "pid=100;uid=0;socket=;"

		cmd.ServeHTTP(rec, req)
		c.Check(handler.lastMethod, check.Equals, method)
		c.Check(rec.Code, check.Equals, 200)
	}

	req, err := http.NewRequest("POTATO", "", nil)
	c.Assert(err, check.IsNil)
	req.RemoteAddr = "pid=100;uid=0;socket=;"

	rec := httptest.NewRecorder()
	cmd.ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 405)
}

func (s *daemonSuite) TestCommandRestartingState(c *check.C) {
	d := s.newDaemon(c)

	cmd := &Command{d: d}
	cmd.GET = func(*Command, *http.Request, *userState) Response {
		return SyncResponse(nil)
	}
	req, err := http.NewRequest("GET", "", nil)
	c.Assert(err, check.IsNil)
	req.RemoteAddr = "pid=100;uid=0;socket=;"

	rec := httptest.NewRecorder()
	cmd.ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 200)
	var rst struct {
		Maintenance *errorResult `json:"maintenance"`
	}
	err = json.Unmarshal(rec.Body.Bytes(), &rst)
	c.Assert(err, check.IsNil)
	c.Check(rst.Maintenance, check.IsNil)

	state.FakeRestarting(d.overlord.State(), state.RestartSystem)
	rec = httptest.NewRecorder()
	cmd.ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 200)
	err = json.Unmarshal(rec.Body.Bytes(), &rst)
	c.Assert(err, check.IsNil)
	c.Check(rst.Maintenance, check.DeepEquals, &errorResult{
		Kind:    errorKindSystemRestart,
		Message: "system is restarting",
	})

	state.FakeRestarting(d.overlord.State(), state.RestartDaemon)
	rec = httptest.NewRecorder()
	cmd.ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 200)
	err = json.Unmarshal(rec.Body.Bytes(), &rst)
	c.Assert(err, check.IsNil)
	c.Check(rst.Maintenance, check.DeepEquals, &errorResult{
		Kind:    errorKindDaemonRestart,
		Message: "daemon is restarting",
	})
}

func (s *daemonSuite) TestFillsWarnings(c *check.C) {
	d := s.newDaemon(c)

	cmd := &Command{d: d}
	cmd.GET = func(*Command, *http.Request, *userState) Response {
		return SyncResponse(nil)
	}
	req, err := http.NewRequest("GET", "", nil)
	c.Assert(err, check.IsNil)
	req.RemoteAddr = "pid=100;uid=0;socket=;"

	rec := httptest.NewRecorder()
	cmd.ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 200)
	var rst struct {
		WarningTimestamp *time.Time `json:"warning-timestamp,omitempty"`
		WarningCount     int        `json:"warning-count,omitempty"`
	}
	err = json.Unmarshal(rec.Body.Bytes(), &rst)
	c.Assert(err, check.IsNil)
	c.Check(rst.WarningCount, check.Equals, 0)
	c.Check(rst.WarningTimestamp, check.IsNil)

	st := d.overlord.State()
	st.Lock()
	st.Warnf("hello world")
	st.Unlock()

	rec = httptest.NewRecorder()
	cmd.ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 200)
	err = json.Unmarshal(rec.Body.Bytes(), &rst)
	c.Assert(err, check.IsNil)
	c.Check(rst.WarningCount, check.Equals, 1)
	c.Check(rst.WarningTimestamp, check.NotNil)
}

func (s *daemonSuite) TestGuestAccess(c *check.C) {
	d := s.newDaemon(c)

	get := &http.Request{Method: "GET"}
	put := &http.Request{Method: "PUT"}
	pst := &http.Request{Method: "POST"}
	del := &http.Request{Method: "DELETE"}

	cmd := &Command{d: d}
	c.Check(cmd.canAccess(get, nil), check.Equals, accessUnauthorized)
	c.Check(cmd.canAccess(put, nil), check.Equals, accessUnauthorized)
	c.Check(cmd.canAccess(pst, nil), check.Equals, accessUnauthorized)
	c.Check(cmd.canAccess(del, nil), check.Equals, accessUnauthorized)

	cmd = &Command{d: d, AdminOnly: true}
	c.Check(cmd.canAccess(get, nil), check.Equals, accessUnauthorized)
	c.Check(cmd.canAccess(put, nil), check.Equals, accessUnauthorized)
	c.Check(cmd.canAccess(pst, nil), check.Equals, accessUnauthorized)
	c.Check(cmd.canAccess(del, nil), check.Equals, accessUnauthorized)

	cmd = &Command{d: d, UserOK: true}
	c.Check(cmd.canAccess(get, nil), check.Equals, accessUnauthorized)
	c.Check(cmd.canAccess(put, nil), check.Equals, accessUnauthorized)
	c.Check(cmd.canAccess(pst, nil), check.Equals, accessUnauthorized)
	c.Check(cmd.canAccess(del, nil), check.Equals, accessUnauthorized)

	cmd = &Command{d: d, GuestOK: true}
	c.Check(cmd.canAccess(get, nil), check.Equals, accessOK)
	c.Check(cmd.canAccess(put, nil), check.Equals, accessUnauthorized)
	c.Check(cmd.canAccess(pst, nil), check.Equals, accessUnauthorized)
	c.Check(cmd.canAccess(del, nil), check.Equals, accessUnauthorized)
}

func (s *daemonSuite) TestUntrustedAccessUntrustedOKWithUser(c *check.C) {
	d := s.newDaemon(c)

	remoteAddr := "pid=100;uid=1000;socket=" + d.untrustedSocketPath + ";"
	get := &http.Request{Method: "GET", RemoteAddr: remoteAddr}
	put := &http.Request{Method: "PUT", RemoteAddr: remoteAddr}
	pst := &http.Request{Method: "POST", RemoteAddr: remoteAddr}
	del := &http.Request{Method: "DELETE", RemoteAddr: remoteAddr}

	cmd := &Command{d: d, UntrustedOK: true}
	c.Check(cmd.canAccess(get, nil), check.Equals, accessOK)
	c.Check(cmd.canAccess(put, nil), check.Equals, accessOK)
	c.Check(cmd.canAccess(pst, nil), check.Equals, accessOK)
	c.Check(cmd.canAccess(del, nil), check.Equals, accessOK)
}

func (s *daemonSuite) TestUntrustedAccessUntrustedOKWithRoot(c *check.C) {
	d := s.newDaemon(c)

	remoteAddr := "pid=100;uid=0;socket=" + d.untrustedSocketPath + ";"
	get := &http.Request{Method: "GET", RemoteAddr: remoteAddr}
	put := &http.Request{Method: "PUT", RemoteAddr: remoteAddr}
	pst := &http.Request{Method: "POST", RemoteAddr: remoteAddr}
	del := &http.Request{Method: "DELETE", RemoteAddr: remoteAddr}

	cmd := &Command{d: d, UntrustedOK: true}
	c.Check(cmd.canAccess(get, nil), check.Equals, accessOK)
	c.Check(cmd.canAccess(put, nil), check.Equals, accessOK)
	c.Check(cmd.canAccess(pst, nil), check.Equals, accessOK)
	c.Check(cmd.canAccess(del, nil), check.Equals, accessOK)
}

func (s *daemonSuite) TestUserAccess(c *check.C) {
	d := s.newDaemon(c)

	get := &http.Request{Method: "GET", RemoteAddr: "pid=100;uid=42;socket=;"}
	put := &http.Request{Method: "PUT", RemoteAddr: "pid=100;uid=42;socket=;"}

	cmd := &Command{d: d}
	c.Check(cmd.canAccess(get, nil), check.Equals, accessUnauthorized)
	c.Check(cmd.canAccess(put, nil), check.Equals, accessUnauthorized)

	cmd = &Command{d: d, AdminOnly: true}
	c.Check(cmd.canAccess(get, nil), check.Equals, accessUnauthorized)
	c.Check(cmd.canAccess(put, nil), check.Equals, accessUnauthorized)

	cmd = &Command{d: d, UserOK: true}
	c.Check(cmd.canAccess(get, nil), check.Equals, accessOK)
	c.Check(cmd.canAccess(put, nil), check.Equals, accessUnauthorized)

	cmd = &Command{d: d, GuestOK: true}
	c.Check(cmd.canAccess(get, nil), check.Equals, accessOK)
	c.Check(cmd.canAccess(put, nil), check.Equals, accessUnauthorized)

	// Since this request has a RemoteAddr, it must be coming from the snapd
	// socket instead of the snap one. In that case, UntrustedOK should have no
	// bearing on the default behavior, which is to deny access.
	cmd = &Command{d: d, UntrustedOK: true}
	c.Check(cmd.canAccess(get, nil), check.Equals, accessUnauthorized)
	c.Check(cmd.canAccess(put, nil), check.Equals, accessUnauthorized)
}

func (s *daemonSuite) TestLoggedInUserAccess(c *check.C) {
	d := s.newDaemon(c)

	user := &userState{}
	get := &http.Request{Method: "GET", RemoteAddr: "pid=100;uid=42;socket=;"}
	put := &http.Request{Method: "PUT", RemoteAddr: "pid=100;uid=42;socket=;"}

	cmd := &Command{d: d}
	c.Check(cmd.canAccess(get, user), check.Equals, accessOK)
	c.Check(cmd.canAccess(put, user), check.Equals, accessOK)

	cmd = &Command{d: d, AdminOnly: true}
	c.Check(cmd.canAccess(get, user), check.Equals, accessUnauthorized)
	c.Check(cmd.canAccess(put, user), check.Equals, accessUnauthorized)

	cmd = &Command{d: d, UserOK: true}
	c.Check(cmd.canAccess(get, user), check.Equals, accessOK)
	c.Check(cmd.canAccess(put, user), check.Equals, accessOK)

	cmd = &Command{d: d, GuestOK: true}
	c.Check(cmd.canAccess(get, user), check.Equals, accessOK)
	c.Check(cmd.canAccess(put, user), check.Equals, accessOK)

	cmd = &Command{d: d, UntrustedOK: true}
	c.Check(cmd.canAccess(get, user), check.Equals, accessOK)
	c.Check(cmd.canAccess(put, user), check.Equals, accessOK)
}

func (s *daemonSuite) TestSuperAccess(c *check.C) {
	d := s.newDaemon(c)

	for _, uid := range []int{0, os.Getuid()} {
		remoteAddr := fmt.Sprintf("pid=100;uid=%d;socket=;", uid)
		get := &http.Request{Method: "GET", RemoteAddr: remoteAddr}
		put := &http.Request{Method: "PUT", RemoteAddr: remoteAddr}

		cmd := &Command{d: d}
		c.Check(cmd.canAccess(get, nil), check.Equals, accessOK)
		c.Check(cmd.canAccess(put, nil), check.Equals, accessOK)

		cmd = &Command{d: d, AdminOnly: true}
		c.Check(cmd.canAccess(get, nil), check.Equals, accessOK)
		c.Check(cmd.canAccess(put, nil), check.Equals, accessOK)

		cmd = &Command{d: d, UserOK: true}
		c.Check(cmd.canAccess(get, nil), check.Equals, accessOK)
		c.Check(cmd.canAccess(put, nil), check.Equals, accessOK)

		cmd = &Command{d: d, GuestOK: true}
		c.Check(cmd.canAccess(get, nil), check.Equals, accessOK)
		c.Check(cmd.canAccess(put, nil), check.Equals, accessOK)

		cmd = &Command{d: d, UntrustedOK: true}
		c.Check(cmd.canAccess(get, nil), check.Equals, accessOK)
		c.Check(cmd.canAccess(put, nil), check.Equals, accessOK)
	}
}

func (s *daemonSuite) TestAddRoutes(c *check.C) {
	d := s.newDaemon(c)

	expected := make([]string, len(api))
	for i, v := range api {
		if v.PathPrefix != "" {
			expected[i] = v.PathPrefix
			continue
		}
		expected[i] = v.Path
	}

	got := make([]string, 0, len(api))
	c.Assert(d.router.Walk(func(route *mux.Route, router *mux.Router, ancestors []*mux.Route) error {
		got = append(got, route.GetName())
		return nil
	}), check.IsNil)

	c.Check(got, check.DeepEquals, expected) // this'll stop being true if routes are added that aren't commands (e.g. for the favicon)
}

type witnessAcceptListener struct {
	net.Listener

	accept  chan struct{}
	accept1 bool

	idempotClose sync.Once
	closeErr     error
	closed       chan struct{}
}

func (l *witnessAcceptListener) Accept() (net.Conn, error) {
	if !l.accept1 {
		l.accept1 = true
		close(l.accept)
	}
	return l.Listener.Accept()
}

func (l *witnessAcceptListener) Close() error {
	l.idempotClose.Do(func() {
		l.closeErr = l.Listener.Close()
		if l.closed != nil {
			close(l.closed)
		}
	})
	return l.closeErr
}

func (s *daemonSuite) TestStartStop(c *check.C) {
	d := s.newDaemon(c)

	l1, err := net.Listen("tcp", "127.0.0.1:0")
	c.Assert(err, check.IsNil)
	l2, err := net.Listen("tcp", "127.0.0.1:0")
	c.Assert(err, check.IsNil)

	generalAccept := make(chan struct{})
	d.generalListener = &witnessAcceptListener{Listener: l1, accept: generalAccept}

	untrustedAccept := make(chan struct{})
	d.untrustedListener = &witnessAcceptListener{Listener: l2, accept: untrustedAccept}

	d.Start()

	generalDone := make(chan struct{})
	go func() {
		select {
		case <-generalAccept:
		case <-time.After(2 * time.Second):
			c.Fatal("general listener accept was not called")
		}
		close(generalDone)
	}()

	untrustedDone := make(chan struct{})
	go func() {
		select {
		case <-untrustedAccept:
		case <-time.After(2 * time.Second):
			c.Fatal("untrusted listener accept was not called")
		}
		close(untrustedDone)
	}()

	<-generalDone
	<-untrustedDone

	err = d.Stop(nil)
	c.Check(err, check.IsNil)
}

func (s *daemonSuite) TestRestartWiring(c *check.C) {
	d := s.newDaemon(c)

	l, err := net.Listen("tcp", "127.0.0.1:0")
	c.Assert(err, check.IsNil)

	generalAccept := make(chan struct{})
	d.generalListener = &witnessAcceptListener{Listener: l, accept: generalAccept}

	untrustedAccept := make(chan struct{})
	d.untrustedListener = &witnessAcceptListener{Listener: l, accept: untrustedAccept}

	d.Start()
	defer d.Stop(nil)

	generalDone := make(chan struct{})
	go func() {
		select {
		case <-generalAccept:
		case <-time.After(2 * time.Second):
			c.Fatal("general accept was not called")
		}
		close(generalDone)
	}()

	untrustedDone := make(chan struct{})
	go func() {
		select {
		case <-untrustedAccept:
		case <-time.After(2 * time.Second):
			c.Fatal("untrusted accept was not called")
		}
		close(untrustedDone)
	}()

	<-generalDone
	<-untrustedDone

	d.overlord.State().RequestRestart(state.RestartDaemon)

	select {
	case <-d.Dying():
	case <-time.After(2 * time.Second):
		c.Fatal("RequestRestart -> overlord -> Kill chain didn't work")
	}
}

func (s *daemonSuite) TestGracefulStop(c *check.C) {
	d := s.newDaemon(c)

	responding := make(chan struct{})
	doRespond := make(chan bool, 1)

	d.router.HandleFunc("/endp", func(w http.ResponseWriter, r *http.Request) {
		close(responding)
		if <-doRespond {
			w.Write([]byte("OKOK"))
		} else {
			w.Write([]byte("Gone"))
		}
		return
	})

	generalL, err := net.Listen("tcp", "127.0.0.1:0")
	c.Assert(err, check.IsNil)

	untrustedL, err := net.Listen("tcp", "127.0.0.1:0")
	c.Assert(err, check.IsNil)

	generalAccept := make(chan struct{})
	generalClosed := make(chan struct{})
	d.generalListener = &witnessAcceptListener{Listener: generalL, accept: generalAccept, closed: generalClosed}

	untrustedAccept := make(chan struct{})
	d.untrustedListener = &witnessAcceptListener{Listener: untrustedL, accept: untrustedAccept}

	d.Start()

	generalAccepting := make(chan struct{})
	go func() {
		select {
		case <-generalAccept:
		case <-time.After(2 * time.Second):
			c.Fatal("general accept was not called")
		}
		close(generalAccepting)
	}()

	untrustedAccepting := make(chan struct{})
	go func() {
		select {
		case <-untrustedAccept:
		case <-time.After(2 * time.Second):
			c.Fatal("general accept was not called")
		}
		close(untrustedAccepting)
	}()

	<-generalAccepting
	<-untrustedAccepting

	alright := make(chan struct{})

	go func() {
		res, err := http.Get(fmt.Sprintf("http://%s/endp", generalL.Addr()))
		c.Assert(err, check.IsNil)
		c.Check(res.StatusCode, check.Equals, 200)
		body, err := ioutil.ReadAll(res.Body)
		res.Body.Close()
		c.Assert(err, check.IsNil)
		c.Check(string(body), check.Equals, "OKOK")
		close(alright)
	}()
	go func() {
		<-generalClosed
		time.Sleep(200 * time.Millisecond)
		doRespond <- true
	}()

	<-responding
	err = d.Stop(nil)
	doRespond <- false
	c.Check(err, check.IsNil)

	select {
	case <-alright:
	case <-time.After(2 * time.Second):
		c.Fatal("never got proper response")
	}
}

func (s *daemonSuite) TestRestartSystemWiring(c *check.C) {
	d := s.newDaemon(c)

	l, err := net.Listen("tcp", "127.0.0.1:0")
	c.Assert(err, check.IsNil)

	generalAccept := make(chan struct{})
	d.generalListener = &witnessAcceptListener{Listener: l, accept: generalAccept}

	untrustedAccept := make(chan struct{})
	d.untrustedListener = &witnessAcceptListener{Listener: l, accept: untrustedAccept}

	d.Start()
	defer d.Stop(nil)

	st := d.overlord.State()

	generalDone := make(chan struct{})
	go func() {
		select {
		case <-generalAccept:
		case <-time.After(2 * time.Second):
			c.Fatal("general accept was not called")
		}
		close(generalDone)
	}()

	untrustedDone := make(chan struct{})
	go func() {
		select {
		case <-untrustedAccept:
		case <-time.After(2 * time.Second):
			c.Fatal("untrusted accept was not called")
		}
		close(untrustedDone)
	}()

	<-generalDone
	<-untrustedDone

	oldRebootNoticeWait := rebootNoticeWait
	oldRebootWaitTimeout := rebootWaitTimeout
	defer func() {
		reboot = rebootImpl
		rebootNoticeWait = oldRebootNoticeWait
		rebootWaitTimeout = oldRebootWaitTimeout
	}()
	rebootWaitTimeout = 100 * time.Millisecond
	rebootNoticeWait = 150 * time.Millisecond

	var delays []time.Duration
	reboot = func(d time.Duration) error {
		delays = append(delays, d)
		return nil
	}

	st.Lock()
	st.RequestRestart(state.RestartSystem)
	st.Unlock()

	defer func() {
		d.mu.Lock()
		d.restartSystem = false
		d.mu.Unlock()
	}()

	select {
	case <-d.Dying():
	case <-time.After(2 * time.Second):
		c.Fatal("RequestRestart -> overlord -> Kill chain didn't work")
	}

	d.mu.Lock()
	rs := d.restartSystem
	d.mu.Unlock()

	c.Check(rs, check.Equals, true)

	c.Check(delays, check.HasLen, 1)
	c.Check(delays[0], check.DeepEquals, rebootWaitTimeout)

	now := time.Now()

	err = d.Stop(nil)

	c.Check(err, check.ErrorMatches, "expected reboot did not happen")

	c.Check(delays, check.HasLen, 2)
	c.Check(delays[1], check.DeepEquals, 1*time.Minute)

	// we are not stopping, we wait for the reboot instead
	c.Check(s.notified, check.DeepEquals, []string{"READY=1"})

	st.Lock()
	defer st.Unlock()
	var rebootAt time.Time
	err = st.Get("daemon-system-restart-at", &rebootAt)
	c.Assert(err, check.IsNil)
	approxAt := now.Add(time.Minute)
	c.Check(rebootAt.After(approxAt) || rebootAt.Equal(approxAt), check.Equals, true)
}

func (s *daemonSuite) TestRebootHelper(c *check.C) {
	cmd := testutil.FakeCommand(c, "shutdown", "")
	defer cmd.Restore()

	tests := []struct {
		delay    time.Duration
		delayArg string
	}{
		{-1, "+0"},
		{0, "+0"},
		{time.Minute, "+1"},
		{10 * time.Minute, "+10"},
		{30 * time.Second, "+0"},
	}

	for _, t := range tests {
		err := reboot(t.delay)
		c.Assert(err, check.IsNil)
		c.Check(cmd.Calls(), check.DeepEquals, [][]string{
			{"shutdown", "-r", t.delayArg, "reboot scheduled to update the system"},
		})

		cmd.ForgetCalls()
	}
}

func makeDaemonListeners(c *check.C, d *Daemon) {
	generalL, err := net.Listen("tcp", "127.0.0.1:0")
	c.Assert(err, check.IsNil)

	untrustedL, err := net.Listen("tcp", "127.0.0.1:0")
	c.Assert(err, check.IsNil)

	generalAccept := make(chan struct{})
	generalClosed := make(chan struct{})
	d.generalListener = &witnessAcceptListener{Listener: generalL, accept: generalAccept, closed: generalClosed}

	untrustedAccept := make(chan struct{})
	d.untrustedListener = &witnessAcceptListener{Listener: untrustedL, accept: untrustedAccept}
}

// This test tests that when a restart of the system is called
// a sigterm (from e.g. systemd) is handled when it arrives before
// stop is fully done.
func (s *daemonSuite) TestRestartShutdownWithSigtermInBetween(c *check.C) {
	oldRebootNoticeWait := rebootNoticeWait
	defer func() {
		rebootNoticeWait = oldRebootNoticeWait
	}()
	rebootNoticeWait = 150 * time.Millisecond

	cmd := testutil.FakeCommand(c, "shutdown", "")
	defer cmd.Restore()

	d := s.newDaemon(c)
	makeDaemonListeners(c, d)

	d.Start()
	st := d.overlord.State()

	st.Lock()
	st.RequestRestart(state.RestartSystem)
	st.Unlock()

	ch := make(chan os.Signal, 2)
	ch <- syscall.SIGTERM
	// stop will check if we got a sigterm in between (which we did)
	err := d.Stop(ch)
	c.Assert(err, check.IsNil)
}

// This test tests that when there is a shutdown we close the sigterm
// handler so that systemd can kill snapd.
func (s *daemonSuite) TestRestartShutdown(c *check.C) {
	oldRebootNoticeWait := rebootNoticeWait
	oldRebootWaitTimeout := rebootWaitTimeout
	defer func() {
		rebootNoticeWait = oldRebootNoticeWait
		rebootWaitTimeout = oldRebootWaitTimeout
	}()
	rebootWaitTimeout = 100 * time.Millisecond
	rebootNoticeWait = 150 * time.Millisecond

	cmd := testutil.FakeCommand(c, "shutdown", "")
	defer cmd.Restore()

	d := s.newDaemon(c)
	makeDaemonListeners(c, d)

	d.Start()
	st := d.overlord.State()

	st.Lock()
	st.RequestRestart(state.RestartSystem)
	st.Unlock()

	sigCh := make(chan os.Signal, 2)
	// stop (this will timeout but thats not relevant for this test)
	d.Stop(sigCh)

	// ensure that the sigCh got closed as part of the stop
	_, chOpen := <-sigCh
	c.Assert(chOpen, check.Equals, false)
}

func (s *daemonSuite) TestRestartExpectedRebootDidNotHappen(c *check.C) {
	curBootID, err := osutil.BootID()
	c.Assert(err, check.IsNil)

	fakeState := []byte(fmt.Sprintf(`{"data":{"patch-level":%d,"patch-sublevel":%d,"some":"data","system-restart-from-boot-id":%q,"daemon-system-restart-at":"%s"},"changes":null,"tasks":null,"last-change-id":0,"last-task-id":0,"last-lane-id":0}`, patch.Level, patch.Sublevel, curBootID, time.Now().UTC().Format(time.RFC3339)))
	err = ioutil.WriteFile(s.statePath, fakeState, 0600)
	c.Assert(err, check.IsNil)

	oldRebootNoticeWait := rebootNoticeWait
	oldRebootRetryWaitTimeout := rebootRetryWaitTimeout
	defer func() {
		rebootNoticeWait = oldRebootNoticeWait
		rebootRetryWaitTimeout = oldRebootRetryWaitTimeout
	}()
	rebootRetryWaitTimeout = 100 * time.Millisecond
	rebootNoticeWait = 150 * time.Millisecond

	cmd := testutil.FakeCommand(c, "shutdown", "")
	defer cmd.Restore()

	d := s.newDaemon(c)
	c.Check(d.overlord, check.IsNil)
	c.Check(d.rebootIsMissing, check.Equals, true)

	var n int
	d.state.Lock()
	err = d.state.Get("daemon-system-restart-tentative", &n)
	d.state.Unlock()
	c.Check(err, check.IsNil)
	c.Check(n, check.Equals, 1)

	d.Start()

	c.Check(s.notified, check.DeepEquals, []string{"READY=1"})

	select {
	case <-d.Dying():
	case <-time.After(2 * time.Second):
		c.Fatal("expected reboot not happening should proceed to try to shutdown again")
	}

	sigCh := make(chan os.Signal, 2)
	// stop (this will timeout but thats not relevant for this test)
	d.Stop(sigCh)

	// an immediate shutdown was scheduled again
	c.Check(cmd.Calls(), check.DeepEquals, [][]string{
		{"shutdown", "-r", "+0", "reboot scheduled to update the system"},
	})
}

func (s *daemonSuite) TestRestartExpectedRebootOK(c *check.C) {
	fakeState := []byte(fmt.Sprintf(`{"data":{"patch-level":%d,"patch-sublevel":%d,"some":"data","system-restart-from-boot-id":%q,"daemon-system-restart-at":"%s"},"changes":null,"tasks":null,"last-change-id":0,"last-task-id":0,"last-lane-id":0}`, patch.Level, patch.Sublevel, "boot-id-0", time.Now().UTC().Format(time.RFC3339)))
	err := ioutil.WriteFile(s.statePath, fakeState, 0600)
	c.Assert(err, check.IsNil)

	cmd := testutil.FakeCommand(c, "shutdown", "")
	defer cmd.Restore()

	d := s.newDaemon(c)
	c.Assert(d.overlord, check.NotNil)

	st := d.overlord.State()
	st.Lock()
	defer st.Unlock()
	var v interface{}
	// these were cleared
	c.Check(st.Get("daemon-system-restart-at", &v), check.Equals, state.ErrNoState)
	c.Check(st.Get("system-restart-from-boot-id", &v), check.Equals, state.ErrNoState)
}

func (s *daemonSuite) TestRestartExpectedRebootGiveUp(c *check.C) {
	// we give up trying to restart the system after 3 retry tentatives
	curBootID, err := osutil.BootID()
	c.Assert(err, check.IsNil)

	fakeState := []byte(fmt.Sprintf(`{"data":{"patch-level":%d,"patch-sublevel":%d,"some":"data","system-restart-from-boot-id":%q,"daemon-system-restart-at":"%s","daemon-system-restart-tentative":3},"changes":null,"tasks":null,"last-change-id":0,"last-task-id":0,"last-lane-id":0}`, patch.Level, patch.Sublevel, curBootID, time.Now().UTC().Format(time.RFC3339)))
	err = ioutil.WriteFile(s.statePath, fakeState, 0600)
	c.Assert(err, check.IsNil)

	cmd := testutil.FakeCommand(c, "shutdown", "")
	defer cmd.Restore()

	d := s.newDaemon(c)
	c.Assert(d.overlord, check.NotNil)

	st := d.overlord.State()
	st.Lock()
	defer st.Unlock()
	var v interface{}
	// these were cleared
	c.Check(st.Get("daemon-system-restart-at", &v), check.Equals, state.ErrNoState)
	c.Check(st.Get("system-restart-from-boot-id", &v), check.Equals, state.ErrNoState)
	c.Check(st.Get("daemon-system-restart-tentative", &v), check.Equals, state.ErrNoState)
}

func (s *daemonSuite) TestRestartIntoSocketModeNoNewChanges(c *check.C) {
	notifySocket := filepath.Join(c.MkDir(), "notify.socket")
	os.Setenv("NOTIFY_SOCKET", notifySocket)
	defer os.Setenv("NOTIFY_SOCKET", "")

	restore := standby.FakeStandbyWait(5 * time.Millisecond)
	defer restore()

	d := s.newDaemon(c)
	makeDaemonListeners(c, d)

	d.Start()

	// pretend some ensure happened
	for i := 0; i < 5; i++ {
		d.overlord.StateEngine().Ensure()
		time.Sleep(5 * time.Millisecond)
	}

	c.Assert(d.standbyOpinions.CanStandby(), check.Equals, false)
	f, _ := os.Create(notifySocket)
	f.Close()
	c.Assert(d.standbyOpinions.CanStandby(), check.Equals, true)

	select {
	case <-d.Dying():
		// exit the loop
	case <-time.After(15 * time.Second):
		c.Errorf("daemon did not stop after 15s")
	}
	err := d.Stop(nil)
	c.Check(err, check.Equals, ErrRestartSocket)
	c.Check(d.restartSocket, check.Equals, true)
}

func (s *daemonSuite) TestRestartIntoSocketModePendingChanges(c *check.C) {
	os.Setenv("NOTIFY_SOCKET", c.MkDir())
	defer os.Setenv("NOTIFY_SOCKET", "")

	restore := standby.FakeStandbyWait(5 * time.Millisecond)
	defer restore()

	d := s.newDaemon(c)
	makeDaemonListeners(c, d)

	st := d.overlord.State()

	d.Start()
	// pretend some ensure happened
	for i := 0; i < 5; i++ {
		d.overlord.StateEngine().Ensure()
		time.Sleep(5 * time.Millisecond)
	}

	select {
	case <-d.Dying():
		// Pretend we got change while shutting down, this can
		// happen when e.g. the user requested a `snap install
		// foo` at the same time as the code in the overlord
		// checked that it can go into socket activated
		// mode. I.e. the daemon was processing the request
		// but no change was generated at the time yet.
		st.Lock()
		chg := st.NewChange("fake-install", "fake install some snap")
		chg.AddTask(st.NewTask("fake-install-task", "fake install task"))
		chgStatus := chg.Status()
		st.Unlock()
		// ensure our change is valid and ready
		c.Check(chgStatus, check.Equals, state.DoStatus)
	case <-time.After(5 * time.Second):
		c.Errorf("daemon did not stop after 5s")
	}
	// when the daemon got a pending change it just restarts
	err := d.Stop(nil)
	c.Check(err, check.IsNil)
	c.Check(d.restartSocket, check.Equals, false)
}

func (s *daemonSuite) TestConnTrackerCanShutdown(c *check.C) {
	ct := &connTracker{conns: make(map[net.Conn]struct{})}
	c.Check(ct.CanStandby(), check.Equals, true)

	con := &net.IPConn{}
	ct.trackConn(con, http.StateActive)
	c.Check(ct.CanStandby(), check.Equals, false)

	ct.trackConn(con, http.StateIdle)
	c.Check(ct.CanStandby(), check.Equals, true)
}

func doTestReq(c *check.C, cmd *Command, mth string) *httptest.ResponseRecorder {
	req, err := http.NewRequest(mth, "", nil)
	c.Assert(err, check.IsNil)
	req.RemoteAddr = "pid=100;uid=0;socket=;"
	rec := httptest.NewRecorder()
	cmd.ServeHTTP(rec, req)
	return rec
}

func (s *daemonSuite) TestDegradedModeReply(c *check.C) {
	d := s.newDaemon(c)
	cmd := &Command{d: d}
	cmd.GET = func(*Command, *http.Request, *userState) Response {
		return SyncResponse(nil)
	}
	cmd.POST = func(*Command, *http.Request, *userState) Response {
		return SyncResponse(nil)
	}

	// pretend we are in degraded mode
	d.SetDegradedMode(fmt.Errorf("foo error"))

	// GET is ok even in degraded mode
	rec := doTestReq(c, cmd, "GET")
	c.Check(rec.Code, check.Equals, 200)
	// POST is not allowed
	rec = doTestReq(c, cmd, "POST")
	c.Check(rec.Code, check.Equals, 500)
	// verify we get the error
	var v struct{ Result errorResult }
	c.Assert(json.NewDecoder(rec.Body).Decode(&v), check.IsNil)
	c.Check(v.Result.Message, check.Equals, "foo error")

	// clean degraded mode
	d.SetDegradedMode(nil)
	rec = doTestReq(c, cmd, "POST")
	c.Check(rec.Code, check.Equals, 200)
}
