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
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/gorilla/mux"
	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/cmd"
	"github.com/canonical/pebble/internals/logger"
	"github.com/canonical/pebble/internals/osutil"
	"github.com/canonical/pebble/internals/overlord"
	"github.com/canonical/pebble/internals/overlord/patch"
	"github.com/canonical/pebble/internals/overlord/restart"
	"github.com/canonical/pebble/internals/overlord/servstate"
	"github.com/canonical/pebble/internals/overlord/standby"
	"github.com/canonical/pebble/internals/overlord/state"
	"github.com/canonical/pebble/internals/reaper"
	"github.com/canonical/pebble/internals/systemd"
	"github.com/canonical/pebble/internals/testutil"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type daemonSuite struct {
	pebbleDir       string
	socketPath      string
	httpAddress     string
	statePath       string
	authorized      bool
	err             error
	notified        []string
	restoreBackends func()
}

var _ = Suite(&daemonSuite{})

func (s *daemonSuite) SetUpTest(c *C) {
	err := reaper.Start()
	if err != nil {
		c.Fatalf("cannot start reaper: %v", err)
	}

	s.socketPath = ""
	s.pebbleDir = c.MkDir()
	s.statePath = filepath.Join(s.pebbleDir, cmd.StateFile)
	systemdSdNotify = func(notif string) error {
		s.notified = append(s.notified, notif)
		return nil
	}
}

func (s *daemonSuite) TearDownTest(c *C) {
	systemdSdNotify = systemd.SdNotify
	s.notified = nil
	s.authorized = false
	s.err = nil

	err := reaper.Stop()
	if err != nil {
		c.Fatalf("cannot stop reaper: %v", err)
	}
}

func (s *daemonSuite) newDaemon(c *C) *Daemon {
	d, err := New(&Options{
		Dir:         s.pebbleDir,
		SocketPath:  s.socketPath,
		HTTPAddress: s.httpAddress,
	})
	c.Assert(err, IsNil)
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

type fakeManager struct {
	id          string
	ensureCalls int
}

func (m *fakeManager) Ensure() error {
	m.ensureCalls++
	return nil
}

type fakeExtension struct {
	mgr fakeManager
}

func (f *fakeExtension) ExtraManagers(o *overlord.Overlord) ([]overlord.StateManager, error) {
	f.mgr = fakeManager{id: "expected", ensureCalls: 0}
	result := []overlord.StateManager{&f.mgr}
	return result, nil
}

type otherFakeExtension struct{}

func (otherFakeExtension) ExtraManagers(o *overlord.Overlord) ([]overlord.StateManager, error) {
	return nil, nil
}

func (s *daemonSuite) TestExternalManager(c *C) {
	d, err := New(&Options{
		Dir:               s.pebbleDir,
		SocketPath:        s.socketPath,
		HTTPAddress:       s.httpAddress,
		OverlordExtension: &fakeExtension{},
	})
	c.Assert(err, IsNil)
	err = d.overlord.StartUp()
	c.Assert(err, IsNil)
	err = d.overlord.StateEngine().Ensure()
	c.Assert(err, IsNil)
	extension, ok := d.overlord.Extension().(*fakeExtension)
	c.Assert(ok, Equals, true)
	manager := extension.mgr
	c.Assert(manager.id, Equals, "expected")
	c.Assert(manager.ensureCalls, Equals, 1)
}

func (s *daemonSuite) TestNoExtension(c *C) {
	d, err := New(&Options{
		Dir:         s.pebbleDir,
		SocketPath:  s.socketPath,
		HTTPAddress: s.httpAddress,
	})
	c.Assert(err, IsNil)

	extension := d.overlord.Extension()
	c.Assert(extension, IsNil)
}

func (s *daemonSuite) TestWrongExtension(c *C) {
	d, err := New(&Options{
		Dir:               s.pebbleDir,
		SocketPath:        s.socketPath,
		HTTPAddress:       s.httpAddress,
		OverlordExtension: &fakeExtension{},
	})
	c.Assert(err, IsNil)

	_, ok := d.overlord.Extension().(*otherFakeExtension)
	c.Assert(ok, Equals, false)
}

func (s *daemonSuite) TestAddCommand(c *C) {
	const endpoint = "/v1/addedendpoint"
	var handler fakeHandler
	getCallback := func(c *Command, r *http.Request, s *UserState) Response {
		handler.cmd = c
		return &handler
	}
	command := Command{
		Path:       endpoint,
		ReadAccess: OpenAccess{},
		GET:        getCallback,
	}
	API = append(API, &command)
	defer func() {
		c.Assert(API[len(API)-1], Equals, &command)
		API = API[:len(API)-1]
	}()

	d := s.newDaemon(c)
	d.Init()
	c.Assert(d.Start(), IsNil)
	defer d.Stop(nil)

	result := d.router.Get(endpoint).GetHandler()
	c.Assert(result, Equals, &command)
}

func (s *daemonSuite) TestExplicitPaths(c *C) {
	s.socketPath = filepath.Join(c.MkDir(), "custom.socket")

	d := s.newDaemon(c)
	d.Init()
	c.Assert(d.Start(), IsNil)
	defer d.Stop(nil)

	info, err := os.Stat(s.socketPath)
	c.Assert(err, IsNil)
	c.Assert(info.Mode(), Equals, os.ModeSocket|0666)
}

func (s *daemonSuite) TestCommandMethodDispatch(c *C) {
	fakeUserAgent := "some-agent-talking-to-pebble/1.0"

	cmd := &Command{d: s.newDaemon(c)}
	handler := &fakeHandler{cmd: cmd}
	rf := func(innerCmd *Command, req *http.Request, user *UserState) Response {
		c.Assert(cmd, Equals, innerCmd)
		return handler
	}
	cmd.GET = rf
	cmd.PUT = rf
	cmd.POST = rf
	cmd.ReadAccess = UserAccess{}
	cmd.WriteAccess = UserAccess{}

	for _, method := range []string{"GET", "POST", "PUT"} {
		req, err := http.NewRequest(method, "", nil)
		req.Header.Add("User-Agent", fakeUserAgent)
		c.Assert(err, IsNil)

		rec := httptest.NewRecorder()
		cmd.ServeHTTP(rec, req)
		c.Check(rec.Code, Equals, 401, Commentf(method))

		rec = httptest.NewRecorder()
		req.RemoteAddr = "pid=100;uid=0;socket=;"

		cmd.ServeHTTP(rec, req)
		c.Check(handler.lastMethod, Equals, method)
		c.Check(rec.Code, Equals, 200)
	}

	req, err := http.NewRequest("POTATO", "", nil)
	c.Assert(err, IsNil)
	req.RemoteAddr = "pid=100;uid=0;socket=;"

	rec := httptest.NewRecorder()
	cmd.ServeHTTP(rec, req)
	c.Check(rec.Code, Equals, 405)
}

func (s *daemonSuite) TestCommandRestartingState(c *C) {
	d := s.newDaemon(c)

	cmd := &Command{d: d, ReadAccess: OpenAccess{}}
	cmd.GET = func(*Command, *http.Request, *UserState) Response {
		return SyncResponse(nil)
	}
	req, err := http.NewRequest("GET", "", nil)
	c.Assert(err, IsNil)
	req.RemoteAddr = "pid=100;uid=0;socket=;"

	rec := httptest.NewRecorder()
	cmd.ServeHTTP(rec, req)
	c.Check(rec.Code, Equals, 200)
	var rst struct {
		Maintenance *errorResult `json:"maintenance"`
	}
	err = json.Unmarshal(rec.Body.Bytes(), &rst)
	c.Assert(err, IsNil)
	c.Check(rst.Maintenance, IsNil)

	state := d.overlord.State()

	state.Lock()
	d.overlord.RestartManager().FakePending(restart.RestartSystem)
	state.Unlock()
	rec = httptest.NewRecorder()
	cmd.ServeHTTP(rec, req)
	c.Check(rec.Code, Equals, 200)
	err = json.Unmarshal(rec.Body.Bytes(), &rst)
	c.Assert(err, IsNil)
	c.Check(rst.Maintenance, DeepEquals, &errorResult{
		Kind:    errorKindSystemRestart,
		Message: "system is restarting",
	})

	state.Lock()
	d.overlord.RestartManager().FakePending(restart.RestartDaemon)
	state.Unlock()
	rec = httptest.NewRecorder()
	cmd.ServeHTTP(rec, req)
	c.Check(rec.Code, Equals, 200)
	err = json.Unmarshal(rec.Body.Bytes(), &rst)
	c.Assert(err, IsNil)
	c.Check(rst.Maintenance, DeepEquals, &errorResult{
		Kind:    errorKindDaemonRestart,
		Message: "daemon is restarting",
	})
}

func (s *daemonSuite) TestFillsWarnings(c *C) {
	d := s.newDaemon(c)

	cmd := &Command{d: d, ReadAccess: OpenAccess{}}
	cmd.GET = func(*Command, *http.Request, *UserState) Response {
		return SyncResponse(nil)
	}
	req, err := http.NewRequest("GET", "", nil)
	c.Assert(err, IsNil)
	req.RemoteAddr = "pid=100;uid=0;socket=;"

	rec := httptest.NewRecorder()
	cmd.ServeHTTP(rec, req)
	c.Check(rec.Code, Equals, 200)
	var rst struct {
		LatestWarning *time.Time `json:"latest-warning"`
	}
	err = json.Unmarshal(rec.Body.Bytes(), &rst)
	c.Assert(err, IsNil)
	c.Check(rst.LatestWarning, IsNil)

	now := time.Now()
	st := d.overlord.State()
	st.Lock()
	st.Warnf("hello world")
	st.Unlock()

	rec = httptest.NewRecorder()
	cmd.ServeHTTP(rec, req)
	c.Check(rec.Code, Equals, 200)
	err = json.Unmarshal(rec.Body.Bytes(), &rst)
	c.Assert(err, IsNil)
	c.Assert(rst.LatestWarning, NotNil)
	c.Check(rst.LatestWarning.Sub(now) < time.Second, Equals, true)
}

type accessCheckerTestCase struct {
	get, put, post int // expected status for each method
	read, write    AccessChecker
}

func (s *daemonSuite) testAccessChecker(c *C, tests []accessCheckerTestCase, remoteAddr string) {
	d := s.newDaemon(c)

	// Add some named identities for testing with.
	d.state.Lock()
	err := d.state.ReplaceIdentities(map[string]*state.Identity{
		"adminuser": {
			Access: state.AdminAccess,
			Local:  &state.LocalIdentity{UserID: 1},
		},
		"readuser": {
			Access: state.ReadAccess,
			Local:  &state.LocalIdentity{UserID: 2},
		},
		"untrusteduser": {
			Access: state.UntrustedAccess,
			Local:  &state.LocalIdentity{UserID: 3},
		},
	})
	d.state.Unlock()
	c.Assert(err, IsNil)

	responseFunc := func(c *Command, r *http.Request, s *UserState) Response {
		return SyncResponse(true)
	}

	doTestReqFunc := func(cmd *Command, mth string) *httptest.ResponseRecorder {
		req := &http.Request{Method: mth, RemoteAddr: remoteAddr}
		rec := httptest.NewRecorder()
		cmd.ServeHTTP(rec, req)
		return rec
	}

	for _, t := range tests {
		cmd := &Command{
			d: d,

			GET:  responseFunc,
			PUT:  responseFunc,
			POST: responseFunc,

			ReadAccess:  t.read,
			WriteAccess: t.write,
		}

		comment := Commentf("remoteAddr: %v, read: %T, write: %T", remoteAddr, t.read, t.write)

		c.Check(doTestReqFunc(cmd, "GET").Code, Equals, t.get, comment)
		c.Check(doTestReqFunc(cmd, "PUT").Code, Equals, t.put, comment)
		c.Check(doTestReqFunc(cmd, "POST").Code, Equals, t.post, comment)
	}
}

func (s *daemonSuite) TestOpenAccess(c *C) {
	tests := []accessCheckerTestCase{{
		get:   http.StatusOK,
		put:   http.StatusOK,
		post:  http.StatusOK,
		read:  OpenAccess{},
		write: OpenAccess{},
	}, {
		get:   http.StatusOK,
		put:   http.StatusUnauthorized,
		post:  http.StatusUnauthorized,
		read:  OpenAccess{},
		write: UserAccess{},
	}, {
		get:   http.StatusOK,
		put:   http.StatusUnauthorized,
		post:  http.StatusUnauthorized,
		read:  OpenAccess{},
		write: AdminAccess{},
	}, {
		get:   http.StatusUnauthorized,
		put:   http.StatusUnauthorized,
		post:  http.StatusUnauthorized,
		read:  UserAccess{},
		write: UserAccess{},
	}, {
		get:   http.StatusUnauthorized,
		put:   http.StatusUnauthorized,
		post:  http.StatusUnauthorized,
		read:  UserAccess{},
		write: AdminAccess{},
	}, {
		get:   http.StatusUnauthorized,
		put:   http.StatusUnauthorized,
		post:  http.StatusUnauthorized,
		read:  AdminAccess{},
		write: AdminAccess{},
	}}

	s.testAccessChecker(c, tests, "")
	s.testAccessChecker(c, tests, "pid=100;uid=3;socket=;") // untrusteduser
}

func (s *daemonSuite) TestUserAccess(c *C) {
	tests := []accessCheckerTestCase{{
		get:   http.StatusOK,
		put:   http.StatusOK,
		post:  http.StatusOK,
		read:  OpenAccess{},
		write: OpenAccess{},
	}, {
		get:   http.StatusOK,
		put:   http.StatusOK,
		post:  http.StatusOK,
		read:  OpenAccess{},
		write: UserAccess{},
	}, {
		get:   http.StatusOK,
		put:   http.StatusUnauthorized,
		post:  http.StatusUnauthorized,
		read:  OpenAccess{},
		write: AdminAccess{},
	}, {
		get:   http.StatusOK,
		put:   http.StatusOK,
		post:  http.StatusOK,
		read:  UserAccess{},
		write: UserAccess{},
	}, {
		get:   http.StatusOK,
		put:   http.StatusUnauthorized,
		post:  http.StatusUnauthorized,
		read:  UserAccess{},
		write: AdminAccess{},
	}, {
		get:   http.StatusUnauthorized,
		put:   http.StatusUnauthorized,
		post:  http.StatusUnauthorized,
		read:  AdminAccess{},
		write: AdminAccess{},
	}}

	s.testAccessChecker(c, tests, "pid=100;uid=42;socket=;")
	s.testAccessChecker(c, tests, "pid=100;uid=2;socket=;") // readuser
}

func (s *daemonSuite) TestAdminAccess(c *C) {
	tests := []accessCheckerTestCase{{
		get:   http.StatusOK,
		put:   http.StatusOK,
		post:  http.StatusOK,
		read:  OpenAccess{},
		write: OpenAccess{},
	}, {
		get:   http.StatusOK,
		put:   http.StatusOK,
		post:  http.StatusOK,
		read:  OpenAccess{},
		write: UserAccess{},
	}, {
		get:   http.StatusOK,
		put:   http.StatusOK,
		post:  http.StatusOK,
		read:  OpenAccess{},
		write: AdminAccess{},
	}, {
		get:   http.StatusOK,
		put:   http.StatusOK,
		post:  http.StatusOK,
		read:  UserAccess{},
		write: UserAccess{},
	}, {
		get:   http.StatusOK,
		put:   http.StatusOK,
		post:  http.StatusOK,
		read:  UserAccess{},
		write: AdminAccess{},
	}, {
		get:   http.StatusOK,
		put:   http.StatusOK,
		post:  http.StatusOK,
		read:  AdminAccess{},
		write: AdminAccess{},
	}}

	s.testAccessChecker(c, tests, "pid=100;uid=0;socket=;")
	s.testAccessChecker(c, tests, "pid=100;uid=1;socket=;") // adminuser
	s.testAccessChecker(c, tests, fmt.Sprintf("pid=100;uid=%d;socket=;", os.Getuid()))
}

func (s *daemonSuite) TestDefaultUcredUsers(c *C) {
	d := s.newDaemon(c)

	var userSeen *UserState
	cmd := &Command{
		d: d,
		GET: func(_ *Command, _ *http.Request, u *UserState) Response {
			userSeen = u
			return SyncResponse(true)
		},
		ReadAccess: UserAccess{},
	}

	// Admin access for UID 0.
	req := &http.Request{Method: "GET", RemoteAddr: "pid=100;uid=0;socket=;"}
	rec := httptest.NewRecorder()
	cmd.ServeHTTP(rec, req)
	c.Check(rec.Code, Equals, http.StatusOK)
	c.Assert(userSeen, NotNil)
	c.Check(userSeen.Access, Equals, state.AdminAccess)
	c.Assert(userSeen.UID, NotNil)
	c.Check(*userSeen.UID, Equals, uint32(0))

	// Admin access for UID == daemon UID.
	userSeen = nil
	req = &http.Request{Method: "GET", RemoteAddr: fmt.Sprintf("pid=100;uid=%d;socket=;", os.Getuid())}
	rec = httptest.NewRecorder()
	cmd.ServeHTTP(rec, req)
	c.Check(rec.Code, Equals, http.StatusOK)
	c.Assert(userSeen, NotNil)
	c.Check(userSeen.Access, Equals, state.AdminAccess)
	c.Assert(userSeen.UID, NotNil)
	c.Check(*userSeen.UID, Equals, uint32(os.Getuid()))

	// Read access for UID not 0 and not daemon UID.
	userSeen = nil
	req = &http.Request{Method: "GET", RemoteAddr: fmt.Sprintf("pid=100;uid=%d;socket=;", os.Getuid()+1)}
	rec = httptest.NewRecorder()
	cmd.ServeHTTP(rec, req)
	c.Check(rec.Code, Equals, http.StatusOK)
	c.Assert(userSeen, NotNil)
	c.Check(userSeen.Access, Equals, state.ReadAccess)
	c.Assert(userSeen.UID, NotNil)
	c.Check(*userSeen.UID, Equals, uint32(os.Getuid()+1))
}

func (s *daemonSuite) TestAddRoutes(c *C) {
	d := s.newDaemon(c)

	expected := make([]string, len(API))
	for i, v := range API {
		if v.PathPrefix != "" {
			expected[i] = v.PathPrefix
			continue
		}
		expected[i] = v.Path
	}

	got := make([]string, 0, len(API))
	c.Assert(d.router.Walk(func(route *mux.Route, router *mux.Router, ancestors []*mux.Route) error {
		got = append(got, route.GetName())
		return nil
	}), IsNil)

	c.Check(got, DeepEquals, expected) // this'll stop being true if routes are added that aren't commands (e.g. for the favicon)
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

func (s *daemonSuite) TestStartStop(c *C) {
	d := s.newDaemon(c)

	l1, err := net.Listen("tcp", "127.0.0.1:0")
	c.Assert(err, IsNil)

	generalAccept := make(chan struct{})
	d.generalListener = &witnessAcceptListener{Listener: l1, accept: generalAccept}

	c.Assert(d.Start(), IsNil)

	generalDone := make(chan struct{})
	go func() {
		select {
		case <-generalAccept:
		case <-time.After(2 * time.Second):
			c.Fatal("general listener accept was not called")
		}
		close(generalDone)
	}()

	<-generalDone

	err = d.Stop(nil)
	c.Check(err, IsNil)
}

func (s *daemonSuite) TestRestartWiring(c *C) {
	d := s.newDaemon(c)

	l, err := net.Listen("tcp", "127.0.0.1:0")
	c.Assert(err, IsNil)

	generalAccept := make(chan struct{})
	d.generalListener = &witnessAcceptListener{Listener: l, accept: generalAccept}

	c.Assert(d.Start(), IsNil)
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

	<-generalDone

	st := d.overlord.State()
	st.Lock()
	restart.Request(st, restart.RestartDaemon)
	st.Unlock()

	select {
	case <-d.Dying():
	case <-time.After(2 * time.Second):
		c.Fatal("RequestRestart -> overlord -> Kill chain didn't work")
	}
}

func (s *daemonSuite) TestGracefulStop(c *C) {
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
	c.Assert(err, IsNil)

	generalAccept := make(chan struct{})
	generalClosed := make(chan struct{})
	d.generalListener = &witnessAcceptListener{Listener: generalL, accept: generalAccept, closed: generalClosed}

	c.Assert(d.Start(), IsNil)

	generalAccepting := make(chan struct{})
	go func() {
		select {
		case <-generalAccept:
		case <-time.After(2 * time.Second):
			c.Fatal("general accept was not called")
		}
		close(generalAccepting)
	}()

	<-generalAccepting

	alright := make(chan struct{})

	go func() {
		res, err := http.Get(fmt.Sprintf("http://%s/endp", generalL.Addr()))
		c.Assert(err, IsNil)
		c.Check(res.StatusCode, Equals, 200)
		body, err := io.ReadAll(res.Body)
		res.Body.Close()
		c.Assert(err, IsNil)
		c.Check(string(body), Equals, "OKOK")
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
	c.Check(err, IsNil)

	select {
	case <-alright:
	case <-time.After(2 * time.Second):
		c.Fatal("never got proper response")
	}
}

func (s *daemonSuite) TestRestartSystemWiring(c *C) {
	d := s.newDaemon(c)

	l, err := net.Listen("tcp", "127.0.0.1:0")
	c.Assert(err, IsNil)

	generalAccept := make(chan struct{})
	d.generalListener = &witnessAcceptListener{Listener: l, accept: generalAccept}

	c.Assert(d.Start(), IsNil)
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

	<-generalDone

	oldRebootNoticeWait := rebootNoticeWait
	oldRebootWaitTimeout := rebootWaitTimeout
	defer func() {
		rebootHandler = systemdModeReboot
		rebootMode = SystemdMode
		rebootNoticeWait = oldRebootNoticeWait
		rebootWaitTimeout = oldRebootWaitTimeout
	}()
	rebootWaitTimeout = 100 * time.Millisecond
	rebootNoticeWait = 150 * time.Millisecond

	var delays []time.Duration
	rebootHandler = func(d time.Duration) error {
		delays = append(delays, d)
		return nil
	}

	st.Lock()
	restart.Request(st, restart.RestartSystem)
	st.Unlock()

	defer func() {
		d.mu.Lock()
		d.requestedRestart = restart.RestartUnset
		d.mu.Unlock()
	}()

	select {
	case <-d.Dying():
	case <-time.After(2 * time.Second):
		c.Fatal("RequestRestart -> overlord -> Kill chain didn't work")
	}

	d.mu.Lock()
	restartType := d.requestedRestart
	d.mu.Unlock()

	c.Check(restartType, Equals, restart.RestartSystem)

	c.Check(delays, HasLen, 1)
	c.Check(delays[0], DeepEquals, rebootWaitTimeout)

	now := time.Now()

	err = d.Stop(nil)

	c.Check(err, ErrorMatches, "expected reboot did not happen")

	c.Check(delays, HasLen, 2)
	c.Check(delays[1], DeepEquals, 1*time.Minute)

	// we are not stopping, we wait for the reboot instead
	c.Check(s.notified, DeepEquals, []string{"READY=1"})

	st.Lock()
	defer st.Unlock()
	var rebootAt time.Time
	err = st.Get("daemon-system-restart-at", &rebootAt)
	c.Assert(err, IsNil)
	approxAt := now.Add(time.Minute)
	c.Check(rebootAt.After(approxAt) || rebootAt.Equal(approxAt), Equals, true)
}

func (s *daemonSuite) TestRebootHelper(c *C) {
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
		err := rebootHandler(t.delay)
		c.Assert(err, IsNil)
		c.Check(cmd.Calls(), DeepEquals, [][]string{
			{"shutdown", "-r", t.delayArg, "reboot scheduled to update the system"},
		})

		cmd.ForgetCalls()
	}
}

func makeDaemonListeners(c *C, d *Daemon) {
	generalL, err := net.Listen("tcp", "127.0.0.1:0")
	c.Assert(err, IsNil)

	generalAccept := make(chan struct{})
	generalClosed := make(chan struct{})
	d.generalListener = &witnessAcceptListener{Listener: generalL, accept: generalAccept, closed: generalClosed}
}

// This test tests that when a restart of the system is called
// a sigterm (from e.g. systemd) is handled when it arrives before
// stop is fully done.
func (s *daemonSuite) TestRestartShutdownWithSigtermInBetween(c *C) {
	oldRebootNoticeWait := rebootNoticeWait
	defer func() {
		rebootNoticeWait = oldRebootNoticeWait
	}()
	rebootNoticeWait = 150 * time.Millisecond

	cmd := testutil.FakeCommand(c, "shutdown", "")
	defer cmd.Restore()

	d := s.newDaemon(c)
	makeDaemonListeners(c, d)

	c.Assert(d.Start(), IsNil)
	st := d.overlord.State()

	st.Lock()
	restart.Request(st, restart.RestartSystem)
	st.Unlock()

	ch := make(chan os.Signal, 2)
	ch <- syscall.SIGTERM
	// stop will check if we got a sigterm in between (which we did)
	err := d.Stop(ch)
	c.Assert(err, IsNil)
}

// This test tests that when there is a shutdown we close the sigterm
// handler so that systemd can kill pebble.
func (s *daemonSuite) TestRestartShutdown(c *C) {
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

	c.Assert(d.Start(), IsNil)
	st := d.overlord.State()

	st.Lock()
	restart.Request(st, restart.RestartSystem)
	st.Unlock()

	sigCh := make(chan os.Signal, 2)
	// stop (this will timeout but thats not relevant for this test)
	d.Stop(sigCh)

	// ensure that the sigCh got closed as part of the stop
	_, chOpen := <-sigCh
	c.Assert(chOpen, Equals, false)
}

func (s *daemonSuite) TestRestartExpectedRebootIsMissing(c *C) {
	curBootID, err := osutil.BootID()
	c.Assert(err, IsNil)

	fakeState := []byte(fmt.Sprintf(`{"data":{"patch-level":%d,"patch-sublevel":%d,"some":"data","system-restart-from-boot-id":%q,"daemon-system-restart-at":"%s"},"changes":null,"tasks":null,"last-change-id":0,"last-task-id":0,"last-lane-id":0}`, patch.Level, patch.Sublevel, curBootID, time.Now().UTC().Format(time.RFC3339)))
	err = os.WriteFile(s.statePath, fakeState, 0600)
	c.Assert(err, IsNil)

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
	c.Check(d.overlord, IsNil)
	c.Check(d.rebootIsMissing, Equals, true)

	var n int
	d.state.Lock()
	err = d.state.Get("daemon-system-restart-tentative", &n)
	d.state.Unlock()
	c.Check(err, IsNil)
	c.Check(n, Equals, 1)

	c.Assert(d.Start(), IsNil)

	c.Check(s.notified, DeepEquals, []string{"READY=1"})

	select {
	case <-d.Dying():
	case <-time.After(2 * time.Second):
		c.Fatal("expected reboot not happening should proceed to try to shutdown again")
	}

	sigCh := make(chan os.Signal, 2)
	// stop (this will timeout but thats not relevant for this test)
	d.Stop(sigCh)

	// an immediate shutdown was scheduled again
	c.Check(cmd.Calls(), DeepEquals, [][]string{
		{"shutdown", "-r", "+0", "reboot scheduled to update the system"},
	})
}

func (s *daemonSuite) TestRestartExpectedRebootOK(c *C) {
	fakeState := []byte(fmt.Sprintf(`{"data":{"patch-level":%d,"patch-sublevel":%d,"some":"data","system-restart-from-boot-id":%q,"daemon-system-restart-at":"%s"},"changes":null,"tasks":null,"last-change-id":0,"last-task-id":0,"last-lane-id":0}`, patch.Level, patch.Sublevel, "boot-id-0", time.Now().UTC().Format(time.RFC3339)))
	err := os.WriteFile(s.statePath, fakeState, 0600)
	c.Assert(err, IsNil)

	cmd := testutil.FakeCommand(c, "shutdown", "")
	defer cmd.Restore()

	d := s.newDaemon(c)
	c.Assert(d.overlord, NotNil)

	st := d.overlord.State()
	st.Lock()
	defer st.Unlock()
	var v any
	// these were cleared
	c.Check(st.Get("daemon-system-restart-at", &v), testutil.ErrorIs, state.ErrNoState)
	c.Check(st.Get("system-restart-from-boot-id", &v), testutil.ErrorIs, state.ErrNoState)
}

func (s *daemonSuite) TestRestartExpectedRebootGiveUp(c *C) {
	// we give up trying to restart the system after 3 retry tentatives
	curBootID, err := osutil.BootID()
	c.Assert(err, IsNil)

	fakeState := []byte(fmt.Sprintf(`{"data":{"patch-level":%d,"patch-sublevel":%d,"some":"data","system-restart-from-boot-id":%q,"daemon-system-restart-at":"%s","daemon-system-restart-tentative":3},"changes":null,"tasks":null,"last-change-id":0,"last-task-id":0,"last-lane-id":0}`, patch.Level, patch.Sublevel, curBootID, time.Now().UTC().Format(time.RFC3339)))
	err = os.WriteFile(s.statePath, fakeState, 0600)
	c.Assert(err, IsNil)

	cmd := testutil.FakeCommand(c, "shutdown", "")
	defer cmd.Restore()

	d := s.newDaemon(c)
	c.Assert(d.overlord, NotNil)

	st := d.overlord.State()
	st.Lock()
	defer st.Unlock()
	var v any
	// these were cleared
	c.Check(st.Get("daemon-system-restart-at", &v), testutil.ErrorIs, state.ErrNoState)
	c.Check(st.Get("system-restart-from-boot-id", &v), testutil.ErrorIs, state.ErrNoState)
	c.Check(st.Get("daemon-system-restart-tentative", &v), testutil.ErrorIs, state.ErrNoState)
}

func (s *daemonSuite) TestRestartIntoSocketModeNoNewChanges(c *C) {
	notifySocket := filepath.Join(c.MkDir(), "notify.socket")
	os.Setenv("NOTIFY_SOCKET", notifySocket)
	defer os.Setenv("NOTIFY_SOCKET", "")

	restore := standby.FakeStandbyWait(5 * time.Millisecond)
	defer restore()

	d := s.newDaemon(c)
	makeDaemonListeners(c, d)

	c.Assert(d.Start(), IsNil)

	// pretend some ensure happened
	for i := 0; i < 5; i++ {
		c.Check(d.overlord.StateEngine().Ensure(), IsNil)
		time.Sleep(5 * time.Millisecond)
	}

	c.Assert(d.standbyOpinions.CanStandby(), Equals, false)
	f, _ := os.Create(notifySocket)
	f.Close()
	c.Assert(d.standbyOpinions.CanStandby(), Equals, true)

	select {
	case <-d.Dying():
		// exit the loop
	case <-time.After(15 * time.Second):
		c.Errorf("daemon did not stop after 15s")
	}
	err := d.Stop(nil)
	c.Check(err, Equals, ErrRestartSocket)
	c.Check(d.requestedRestart, Equals, restart.RestartSocket)
}

func (s *daemonSuite) TestRestartIntoSocketModePendingChanges(c *C) {
	os.Setenv("NOTIFY_SOCKET", c.MkDir())
	defer os.Setenv("NOTIFY_SOCKET", "")

	restore := standby.FakeStandbyWait(5 * time.Millisecond)
	defer restore()

	d := s.newDaemon(c)
	makeDaemonListeners(c, d)

	st := d.overlord.State()

	c.Assert(d.Start(), IsNil)
	// pretend some ensure happened
	for i := 0; i < 5; i++ {
		c.Check(d.overlord.StateEngine().Ensure(), IsNil)
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
		c.Check(chgStatus, Equals, state.DoStatus)
	case <-time.After(5 * time.Second):
		c.Errorf("daemon did not stop after 5s")
	}
	// when the daemon got a pending change it just restarts
	err := d.Stop(nil)
	c.Check(err, IsNil)
	c.Check(d.requestedRestart, Equals, restart.RestartDaemon)
}

func (s *daemonSuite) TestRestartServiceFailure(c *C) {
	writeTestLayer(s.pebbleDir, `
services:
    test1:
        override: replace
        command: /bin/sh -c 'sleep 1.5; exit 1'
        on-failure: shutdown
`)
	d := s.newDaemon(c)
	err := d.Init()
	c.Assert(err, IsNil)
	c.Assert(d.Start(), IsNil)

	// Start the test service.
	payload := bytes.NewBufferString(`{"action": "start", "services": ["test1"]}`)
	req, err := http.NewRequest("POST", "/v1/services", payload)
	c.Assert(err, IsNil)
	rsp := v1PostServices(apiCmd("/v1/services"), req, nil).(*resp)
	rec := httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)
	c.Check(rec.Result().StatusCode, Equals, 202)

	// We have to wait for it be in running state.
	for i := 0; ; i++ {
		if i >= 25 {
			c.Fatalf("timed out waiting for service to start")
		}
		d.state.Lock()
		change := d.state.Change(rsp.Change)
		d.state.Unlock()
		if change != nil && change.IsReady() {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Wait for daemon to be shut down by the failed service.
	select {
	case <-d.Dying():
	case <-time.After(2 * time.Second):
		c.Fatalf("timed out waiting for ")
	}

	// Ensure it returned a service-failure error.
	err = d.Stop(nil)
	c.Assert(err, Equals, ErrRestartServiceFailure)
}

func (s *daemonSuite) TestRebootExternal(c *C) {
	oldRebootWaitTimeout := rebootWaitTimeout
	defer func() {
		rebootWaitTimeout = oldRebootWaitTimeout
	}()
	rebootWaitTimeout = 0

	didFallbackReboot := false
	defer FakeSyscallSync(func() {})()
	defer FakeSyscallReboot(func(cmd int) error {
		if cmd == syscall.LINUX_REBOOT_CMD_RESTART {
			didFallbackReboot = true
		}
		return nil
	})()
	SetRebootMode(ExternalMode)
	defer SetRebootMode(SystemdMode)

	d := s.newDaemon(c)
	makeDaemonListeners(c, d)
	c.Assert(d.Start(), IsNil)

	st := d.overlord.State()
	st.Lock()
	restart.Request(st, restart.RestartSystem)
	st.Unlock()

	select {
	case <-d.Dying():
	case <-time.After(2 * time.Second):
		c.Fatal("RequestRestart -> overlord -> Kill chain didn't work")
	}

	d.mu.Lock()
	restartType := d.requestedRestart
	d.mu.Unlock()

	c.Assert(restartType, Equals, restart.RestartSystem)

	err := d.Stop(nil)
	c.Assert(errors.Is(err, ErrRestartExternal), Equals, true)

	d.mu.Lock()
	d.requestedRestart = restart.RestartUnset
	d.mu.Unlock()

	c.Assert(didFallbackReboot, Equals, true)
	st.Lock()
	restart.ClearReboot(st)
	st.Unlock()
}

func (s *daemonSuite) TestConnTrackerCanShutdown(c *C) {
	ct := &connTracker{conns: make(map[net.Conn]struct{})}
	c.Check(ct.CanStandby(), Equals, true)

	con := &net.IPConn{}
	ct.trackConn(con, http.StateActive)
	c.Check(ct.CanStandby(), Equals, false)

	ct.trackConn(con, http.StateIdle)
	c.Check(ct.CanStandby(), Equals, true)
}

func doTestReq(c *C, cmd *Command, mth string) *httptest.ResponseRecorder {
	req, err := http.NewRequest(mth, "", nil)
	c.Assert(err, IsNil)
	req.RemoteAddr = "pid=100;uid=0;socket=;"
	rec := httptest.NewRecorder()
	cmd.ServeHTTP(rec, req)
	return rec
}

func (s *daemonSuite) TestDegradedModeReply(c *C) {
	d := s.newDaemon(c)
	cmd := &Command{d: d, ReadAccess: OpenAccess{}, WriteAccess: OpenAccess{}}
	cmd.GET = func(*Command, *http.Request, *UserState) Response {
		return SyncResponse(nil)
	}
	cmd.POST = func(*Command, *http.Request, *UserState) Response {
		return SyncResponse(nil)
	}

	// pretend we are in degraded mode
	d.SetDegradedMode(fmt.Errorf("foo error"))

	// GET is ok even in degraded mode
	rec := doTestReq(c, cmd, "GET")
	c.Check(rec.Code, Equals, 200)
	// POST is not allowed
	rec = doTestReq(c, cmd, "POST")
	c.Check(rec.Code, Equals, 500)
	// verify we get the error
	var v struct{ Result errorResult }
	c.Assert(json.NewDecoder(rec.Body).Decode(&v), IsNil)
	c.Check(v.Result.Message, Equals, "foo error")

	// clean degraded mode
	d.SetDegradedMode(nil)
	rec = doTestReq(c, cmd, "POST")
	c.Check(rec.Code, Equals, 200)
}

func (s *daemonSuite) TestHTTPAPI(c *C) {
	s.httpAddress = ":0" // Go will choose port (use listener.Addr() to find it)
	d := s.newDaemon(c)
	d.Init()
	c.Assert(d.Start(), IsNil)
	port := d.httpListener.Addr().(*net.TCPAddr).Port

	request, err := http.NewRequest("GET", fmt.Sprintf("http://localhost:%d/v1/health", port), nil)
	c.Assert(err, IsNil)
	response, err := http.DefaultClient.Do(request)
	c.Assert(err, IsNil)
	c.Assert(response.StatusCode, Equals, http.StatusOK)
	var m map[string]any
	err = json.NewDecoder(response.Body).Decode(&m)
	c.Assert(err, IsNil)
	c.Assert(m, DeepEquals, map[string]any{
		"type":        "sync",
		"status-code": float64(http.StatusOK),
		"status":      "OK",
		"result": map[string]any{
			"healthy": true,
		},
	})

	request, err = http.NewRequest("GET", fmt.Sprintf("http://localhost:%d/v1/checks", port), nil)
	c.Assert(err, IsNil)
	response, err = http.DefaultClient.Do(request)
	c.Assert(err, IsNil)
	c.Assert(response.StatusCode, Equals, http.StatusUnauthorized)

	err = d.Stop(nil)
	c.Assert(err, IsNil)
	_, err = http.DefaultClient.Do(request)
	c.Assert(err, ErrorMatches, ".* connection refused")
}

func (s *daemonSuite) TestStopRunning(c *C) {
	// Start the daemon.
	writeTestLayer(s.pebbleDir, `
services:
    test1:
        override: replace
        command: sleep 10
`)
	d := s.newDaemon(c)
	err := d.Init()
	c.Assert(err, IsNil)
	c.Assert(d.Start(), IsNil)

	// Start the test service.
	payload := bytes.NewBufferString(`{"action": "start", "services": ["test1"]}`)
	req, err := http.NewRequest("POST", "/v1/services", payload)
	c.Assert(err, IsNil)
	rsp := v1PostServices(apiCmd("/v1/services"), req, nil).(*resp)
	rec := httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)
	c.Check(rec.Result().StatusCode, Equals, 202)

	// We have to wait for it be in running state for StopRunning to stop it.
	for i := 0; ; i++ {
		if i >= 25 {
			c.Fatalf("timed out waiting for service to start")
		}
		d.state.Lock()
		change := d.state.Change(rsp.Change)
		d.state.Unlock()
		if change != nil && change.IsReady() {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Stop the daemon (which should shut down the service manager and stop services).
	err = d.Stop(nil)
	c.Assert(err, IsNil)

	// Ensure the "stop" change was created, along with its "stop" tasks.
	d.state.Lock()
	defer d.state.Unlock()
	changes := d.state.Changes()
	var change *state.Change
	for _, ch := range changes {
		if ch.Kind() == "stop" {
			change = ch
		}
	}
	if change == nil {
		c.Fatalf("stop change not found")
	}
	c.Check(change.Status(), Equals, state.DoneStatus)
	tasks := change.Tasks()
	c.Assert(tasks, HasLen, 1)
	c.Check(tasks[0].Kind(), Equals, "stop")
}

func (s *daemonSuite) TestStopWithinOkayDelay(c *C) {
	// Start the daemon.
	writeTestLayer(s.pebbleDir, `
services:
    test1:
        override: replace
        command: sleep 10
`)
	d := s.newDaemon(c)
	err := d.Init()
	c.Assert(err, IsNil)
	c.Assert(d.Start(), IsNil)

	// Start the test service.
	payload := bytes.NewBufferString(`{"action": "start", "services": ["test1"]}`)
	req, err := http.NewRequest("POST", "/v1/services", payload)
	c.Assert(err, IsNil)
	rsp := v1PostServices(apiCmd("/v1/services"), req, nil).(*resp)
	rec := httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)
	c.Check(rec.Result().StatusCode, Equals, 202)

	// Waiting for the change to be in doing state cannot guarantee the service is
	// in the starting state, so here we wait until the service is in the starting
	// state. We wait up to 25*20=500ms to make sure there is still half a second
	// left to stop the service before okayDelay.
	for i := 0; i < 25; i++ {
		svcInfo, err := d.overlord.ServiceManager().Services([]string{"test1"})
		c.Assert(err, IsNil)
		if len(svcInfo) > 0 && svcInfo[0].Current == servstate.StatusActive {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Stop the daemon, which should stop services in starting state. At this point,
	// it should still be within the okayDelay.
	err = d.Stop(nil)
	c.Assert(err, IsNil)

	// Ensure a "stop" change is created, along with its "stop" tasks.
	d.state.Lock()
	defer d.state.Unlock()
	changes := d.state.Changes()
	var change *state.Change
	for _, ch := range changes {
		if ch.Kind() == "stop" {
			change = ch
		}
	}
	if change == nil {
		c.Fatalf("stop change not found")
	}
	c.Check(change.Status(), Equals, state.DoneStatus)
	tasks := change.Tasks()
	c.Assert(tasks, HasLen, 1)
	c.Check(tasks[0].Kind(), Equals, "stop")
}

func (s *daemonSuite) TestWritesRequireAdminAccess(c *C) {
	for _, cmd := range API {
		if cmd.Path == "/v1/notices" {
			// Any user is allowed to add a notice with their own uid.
			continue
		}
		switch cmd.WriteAccess.(type) {
		case OpenAccess, UserAccess:
			c.Errorf("%s WriteAccess should be AdminAccess, not %T", cmd.Path, cmd.WriteAccess)
		}
	}

	// File pull (read) may be sensitive, so requires admin access too.
	cmd := apiCmd("/v1/files")
	switch cmd.ReadAccess.(type) {
	case OpenAccess, UserAccess:
		c.Errorf("%s ReadAccess should be AdminAccess, not %T", cmd.Path, cmd.WriteAccess)
	}

	// Task websockets (GET) is used for exec, so requires admin access too.
	cmd = apiCmd("/v1/tasks/{task-id}/websocket/{websocket-id}")
	switch cmd.ReadAccess.(type) {
	case OpenAccess, UserAccess:
		c.Errorf("%s ReadAccess should be AdminAccess, not %T", cmd.Path, cmd.WriteAccess)
	}
}

func (s *daemonSuite) TestAPIAccessLevels(c *C) {
	_ = s.newDaemon(c)

	tests := []struct {
		method string
		path   string
		body   string
		uid    int // -1 means no peer cred user
		status int
	}{
		{"GET", "/v1/system-info", ``, -1, http.StatusOK},

		{"GET", "/v1/health", ``, -1, http.StatusOK},

		{"GET", "/v1/changes", ``, -1, http.StatusUnauthorized},
		{"GET", "/v1/changes", ``, 42, http.StatusOK},
		{"GET", "/v1/changes", ``, 0, http.StatusOK},

		{"GET", "/v1/services", ``, -1, http.StatusUnauthorized},
		{"GET", "/v1/services", ``, 42, http.StatusOK},
		{"GET", "/v1/services", ``, 0, http.StatusOK},
		{"POST", "/v1/services", ``, -1, http.StatusUnauthorized},
		{"POST", "/v1/services", ``, 42, http.StatusUnauthorized},
		{"POST", "/v1/services", ``, 0, http.StatusBadRequest},

		{"POST", "/v1/layers", ``, -1, http.StatusUnauthorized},
		{"POST", "/v1/layers", ``, 42, http.StatusUnauthorized},
		{"POST", "/v1/layers", ``, 0, http.StatusBadRequest},

		{"GET", "/v1/files?action=list&path=/", ``, -1, http.StatusUnauthorized},
		{"GET", "/v1/files?action=list&path=/", ``, 42, http.StatusUnauthorized}, // even reading files requires admin
		{"GET", "/v1/files?action=list&path=/", ``, 0, http.StatusOK},
		{"POST", "/v1/files", `{}`, -1, http.StatusUnauthorized},
		{"POST", "/v1/files", `{}`, 42, http.StatusUnauthorized},
		{"POST", "/v1/files", `{}`, 0, http.StatusBadRequest},

		{"GET", "/v1/logs", ``, -1, http.StatusUnauthorized},
		{"GET", "/v1/logs", ``, 42, http.StatusOK},
		{"GET", "/v1/logs", ``, 0, http.StatusOK},

		{"POST", "/v1/exec", `{}`, -1, http.StatusUnauthorized},
		{"POST", "/v1/exec", `{}`, 42, http.StatusUnauthorized},
		{"POST", "/v1/exec", `{}`, 0, http.StatusBadRequest},

		{"POST", "/v1/signals", `{}`, -1, http.StatusUnauthorized},
		{"POST", "/v1/signals", `{}`, 42, http.StatusUnauthorized},
		{"POST", "/v1/signals", `{}`, 0, http.StatusBadRequest},

		{"GET", "/v1/checks", ``, -1, http.StatusUnauthorized},
		{"GET", "/v1/checks", ``, 42, http.StatusOK},
		{"GET", "/v1/checks", ``, 0, http.StatusOK},

		{"GET", "/v1/notices", ``, -1, http.StatusUnauthorized},
		{"GET", "/v1/notices", ``, 42, http.StatusOK},
		{"GET", "/v1/notices", ``, 0, http.StatusOK},
		{"POST", "/v1/notices", `{}`, -1, http.StatusUnauthorized},
		{"POST", "/v1/notices", `{}`, 42, http.StatusBadRequest},
		{"POST", "/v1/notices", `{}`, 0, http.StatusBadRequest},
	}

	for _, test := range tests {
		remoteAddr := ""
		if test.uid >= 0 {
			remoteAddr = fmt.Sprintf("pid=100;uid=%d;socket=;", test.uid)
		}
		requestURL, err := url.Parse("http://localhost" + test.path)
		c.Assert(err, IsNil)
		request := &http.Request{
			Method:     test.method,
			URL:        requestURL,
			Body:       io.NopCloser(strings.NewReader(test.body)),
			RemoteAddr: remoteAddr,
		}
		recorder := httptest.NewRecorder()
		cmd := apiCmd(requestURL.Path)
		cmd.ServeHTTP(recorder, request)

		response := recorder.Result()
		if response.StatusCode != test.status {
			// Log response body to make it easier to debug if the test fails.
			c.Logf("%s %s uid=%d: expected %d, got %d; response body:\n%s",
				test.method, test.path, test.uid, test.status, response.StatusCode, recorder.Body.String())
		}
		c.Assert(response.StatusCode, Equals, test.status)
	}
}

type rebootSuite struct{}

var _ = Suite(&rebootSuite{})

func (s *rebootSuite) TestSyscallPosRebootDelay(c *C) {
	wait := make(chan struct{})
	defer FakeSyscallSync(func() {})()
	defer FakeSyscallReboot(func(cmd int) error {
		if cmd == syscall.LINUX_REBOOT_CMD_RESTART {
			close(wait)
		}
		return nil
	})()

	period := 25 * time.Millisecond
	syscallModeReboot(period)
	start := time.Now()
	select {
	case <-wait:
	case <-time.After(10 * time.Second):
		c.Fatal("syscall did not take place and we timed out")
	}
	elapsed := time.Now().Sub(start)
	c.Assert(elapsed >= period, Equals, true)
}

func (s *rebootSuite) TestSyscallNegRebootDelay(c *C) {
	wait := make(chan struct{})
	defer FakeSyscallSync(func() {})()
	defer FakeSyscallReboot(func(cmd int) error {
		if cmd == syscall.LINUX_REBOOT_CMD_RESTART {
			close(wait)
		}
		return nil
	})()

	// Negative periods will be zeroed, so do not fear the huge negative.
	// We do supply a rather big value here because this test is
	// effectively a race, but given the huge timeout, it is not going
	// to be a problem (c).
	period := 10 * time.Second
	go func() {
		// We need a different thread for the unbuffered wait.
		syscallModeReboot(-period)
	}()
	start := time.Now()
	select {
	case <-wait:
	case <-time.After(10 * time.Second):
		c.Fatal("syscall did not take place and we timed out")
	}
	elapsed := time.Now().Sub(start)
	c.Assert(elapsed < period, Equals, true)
}

func (s *rebootSuite) TestSetSyscall(c *C) {
	wait := make(chan struct{})
	defer FakeSyscallSync(func() {})()
	defer FakeSyscallReboot(func(cmd int) error {
		if cmd == syscall.LINUX_REBOOT_CMD_RESTART {
			close(wait)
		}
		return nil
	})()

	// We know the default is systemdReboot otherwise the unit tests
	// above will fail. We need to check the switch works.
	SetRebootMode(SyscallMode)
	defer SetRebootMode(SystemdMode)

	err := make(chan error)
	go func() {
		// We need a different thread for the unbuffered wait.
		err <- rebootHandler(0)
	}()

	select {
	case <-wait:
	case <-time.After(10 * time.Second):
		c.Fatal("syscall did not take place and we timed out")
	}
	c.Assert(<-err, IsNil)
}

type fakeLogger struct {
	msg      string
	noticeCh chan int
}

func (f *fakeLogger) Notice(msg string) {
	f.msg = msg
	f.noticeCh <- 1
}

func (f *fakeLogger) Debug(msg string) {}

func (s *rebootSuite) TestSyscallRebootError(c *C) {
	defer FakeSyscallSync(func() {})()
	defer FakeSyscallReboot(func(cmd int) error {
		return fmt.Errorf("-EPERM")
	})()

	// We know the default is systemdReboot otherwise the unit tests
	// above will fail. We need to check the switch works.
	SetRebootMode(SyscallMode)
	defer SetRebootMode(SystemdMode)

	complete := make(chan int)
	l := fakeLogger{noticeCh: complete}
	old := logger.SetLogger(&l)
	defer logger.SetLogger(old)

	err := make(chan error)
	go func() {
		// We need a different thread for the unbuffered wait.
		err <- rebootHandler(0)
	}()
	select {
	case <-complete:
	case <-time.After(10 * time.Second):
		c.Fatal("syscall did not take place and we timed out")
	}
	c.Assert(l.msg, Matches, "*-EPERM")
	c.Assert(<-err, IsNil)
}

type utilsSuite struct{}

var _ = Suite(&utilsSuite{})

func (s *utilsSuite) TestExitOnPanic(c *C) {
	// Non-panicking handler shouldn't exit
	normalHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(r.URL.Path))
	})
	var stderr bytes.Buffer
	exited := false
	exit := func() {
		exited = true
	}
	recorder := httptest.NewRecorder()
	wrapped := exitOnPanic(normalHandler, &stderr, exit)
	wrapped.ServeHTTP(recorder, httptest.NewRequest("GET", "/normal", nil))
	body, err := io.ReadAll(recorder.Result().Body)
	c.Assert(err, IsNil)
	c.Check(string(body), Equals, "/normal")
	c.Check(stderr.String(), Equals, "")
	c.Check(exited, Equals, false)

	// Panicking handler should exit
	panicHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("before"))
		panic("PANIC!")
	})
	stderr.Reset()
	recorder = httptest.NewRecorder()
	wrapped = exitOnPanic(panicHandler, &stderr, exit)
	wrapped.ServeHTTP(recorder, httptest.NewRequest("GET", "/panic", nil))
	body, err = io.ReadAll(recorder.Result().Body)
	c.Assert(err, IsNil)
	c.Check(string(body), Equals, "before")
	c.Check(stderr.String(), Matches, "(?s)panic: PANIC!.*goroutine.*")
	c.Check(exited, Equals, true)
}
