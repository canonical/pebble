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
	"errors"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"sync"
	"syscall"
)

var errNoID = errors.New("no pid/uid found")

const (
	// ucrednetNoProcess is -1 to avoid the case when pid=0 is returned from the kernel
	// because the connecting process is in an unrelated process namespace.
	ucrednetNoProcess = int32(-1)
	ucrednetNobody    = uint32((1 << 32) - 1)
)

var raddrRegexp = regexp.MustCompile(`^pid=(\d+);uid=(\d+);socket=([^;]*);$`)

func ucrednetGet(remoteAddr string) (*ucrednet, error) {
	// NOTE treat remoteAddr at one point included a user-controlled
	// string. In case that happens again by accident, treat it as tainted,
	// and be very suspicious of it.
	u := &ucrednet{
		Pid: ucrednetNoProcess,
		Uid: ucrednetNobody,
	}
	subs := raddrRegexp.FindStringSubmatch(remoteAddr)
	if subs != nil {
		if v, err := strconv.ParseInt(subs[1], 10, 32); err == nil {
			u.Pid = int32(v)
		}
		if v, err := strconv.ParseUint(subs[2], 10, 32); err == nil {
			u.Uid = uint32(v)
		}
		u.Socket = subs[3]
	}
	if u.Pid == ucrednetNoProcess || u.Uid == ucrednetNobody {
		return nil, errNoID
	}

	return u, nil
}

type ucrednet struct {
	Pid    int32
	Uid    uint32
	Socket string
}

func (un *ucrednet) String() string {
	if un == nil {
		return "pid=;uid=;socket=;"
	}
	return fmt.Sprintf("pid=%d;uid=%d;socket=%s;", un.Pid, un.Uid, un.Socket)
}

type ucrednetAddr struct {
	net.Addr
	*ucrednet
}

func (wa *ucrednetAddr) String() string {
	// NOTE we drop the original (user-supplied) net.Addr from the
	// serialization entirely. We carry it this far so it helps debugging
	// (via %#v logging), but from here on in it's not helpful.
	return wa.ucrednet.String()
}

type ucrednetConn struct {
	net.Conn
	*ucrednet
}

func (wc *ucrednetConn) RemoteAddr() net.Addr {
	return &ucrednetAddr{wc.Conn.RemoteAddr(), wc.ucrednet}
}

type ucrednetListener struct {
	net.Listener

	idempotClose sync.Once
	closeErr     error
}

var getUcred = syscall.GetsockoptUcred

func (wl *ucrednetListener) Accept() (net.Conn, error) {
	con, err := wl.Listener.Accept()
	if err != nil {
		return nil, err
	}

	var unet *ucrednet
	if ucon, ok := con.(*net.UnixConn); ok {
		rawConn, err := ucon.SyscallConn()
		if err != nil {
			return nil, err
		}
		var ucred *syscall.Ucred
		var ucredErr error
		// Call getUcred inside a Control() block to ensure fd is valid for
		// the duration of the call, avoiding a race condition.
		err = rawConn.Control(func(fd uintptr) {
			ucred, ucredErr = getUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
		})
		if err != nil {
			return nil, err
		}
		if ucredErr != nil {
			return nil, ucredErr
		}
		unet = &ucrednet{
			Pid:    ucred.Pid,
			Uid:    ucred.Uid,
			Socket: ucon.LocalAddr().String(),
		}
	}

	return &ucrednetConn{con, unet}, nil
}

func (wl *ucrednetListener) Close() error {
	wl.idempotClose.Do(func() {
		wl.closeErr = wl.Listener.Close()
	})
	return wl.closeErr
}
