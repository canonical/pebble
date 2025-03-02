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

package osutil

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"syscall"
	"time"

	"github.com/canonical/pebble/internals/osutil/sys"
)

func FakeUserCurrent(f func() (*user.User, error)) func() {
	realUserCurrent := userCurrent
	userCurrent = f

	return func() { userCurrent = realUserCurrent }
}

func FakeUserLookup(f func(name string) (*user.User, error)) func() {
	oldUserLookup := userLookup
	userLookup = f
	return func() { userLookup = oldUserLookup }
}

func FakeUserLookupId(f func(name string) (*user.User, error)) func() {
	oldUserLookupId := userLookupId
	userLookupId = f
	return func() { userLookupId = oldUserLookupId }
}

func FakeUserLookupGroup(f func(name string) (*user.Group, error)) func() {
	oldUserLookupGroup := userLookupGroup
	userLookupGroup = f
	return func() { userLookupGroup = oldUserLookupGroup }
}

func FakeChown(f func(*os.File, sys.UserID, sys.GroupID) error) (restore func()) {
	oldChown := chown
	chown = f
	return func() {
		chown = oldChown
	}
}

// FakeMountInfo fakes content of /proc/self/mountinfo.
func FakeMountInfo(text string) (restore func()) {
	old := procSelfMountInfo
	f, err := os.CreateTemp("", "mountinfo")
	if err != nil {
		panic(fmt.Errorf("cannot open temporary file: %s", err))
	}
	if err := os.WriteFile(f.Name(), []byte(text), 0644); err != nil {
		panic(fmt.Errorf("cannot write mock mountinfo file: %s", err))
	}
	procSelfMountInfo = f.Name()
	return func() {
		os.Remove(f.Name())
		procSelfMountInfo = old
	}
}

func SetUnsafeIO(b bool) (restore func()) {
	old := unsafeIO
	unsafeIO = b
	return func() {
		unsafeIO = old
	}
}

func SetAtomicFileRenamed(aw *AtomicFile, renamed bool) {
	aw.renamed = renamed
}

func WaitingReaderGuts(r io.Reader) (io.Reader, *exec.Cmd) {
	wr := r.(*waitingReader)
	return wr.reader, wr.cmd
}

func FakeCmdWaitTimeout(timeout time.Duration) (restore func()) {
	oldCmdWaitTimeout := cmdWaitTimeout
	cmdWaitTimeout = timeout
	return func() {
		cmdWaitTimeout = oldCmdWaitTimeout
	}
}

func FakeSyscallKill(f func(int, syscall.Signal) error) (restore func()) {
	oldSyscallKill := syscallKill
	syscallKill = f
	return func() {
		syscallKill = oldSyscallKill
	}
}

func FakeSyscallGetpgid(f func(int) (int, error)) (restore func()) {
	oldSyscallGetpgid := syscallGetpgid
	syscallGetpgid = f
	return func() {
		syscallGetpgid = oldSyscallGetpgid
	}
}

func FakeEnviron(f func() []string) (restore func()) {
	oldEnviron := osEnviron
	osEnviron = f
	return func() {
		osEnviron = oldEnviron
	}
}

func FakeRandomString(f func(int) string) (restore func()) {
	old := randomString
	randomString = f
	return func() {
		randomString = old
	}
}
