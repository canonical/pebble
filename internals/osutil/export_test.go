// Copyright (C) 2016 Canonical Ltd
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
	"syscall"
	"time"

	"github.com/canonical/pebble/internals/osutil/sys"
	"github.com/canonical/pebble/internals/osutil/user"
	"github.com/canonical/pebble/internals/testutil"
	"github.com/canonical/x-go/strutil"
)

var (
	StreamsEqualChunked  = streamsEqualChunked
	FilesAreEqualChunked = filesAreEqualChunked
	SudoersFile          = sudoersFile
	DoCopyFile           = doCopyFile
)

type Fileish = fileish

func FakeMaxCp(new int64) (restore func()) {
	old := maxcp
	maxcp = new
	return func() {
		maxcp = old
	}
}

func FakeCopyFile(new func(fileish, fileish, os.FileInfo) error) (restore func()) {
	old := copyfile
	copyfile = new
	return func() {
		copyfile = old
	}
}

func FakeOpenFile(new func(string, int, os.FileMode) (fileish, error)) (restore func()) {
	old := openfile
	openfile = new
	return func() {
		openfile = old
	}
}

func FakeSyscallSettimeofday(f func(*syscall.Timeval) error) (restore func()) {
	old := syscallSettimeofday
	syscallSettimeofday = f
	return func() {
		syscallSettimeofday = old
	}
}

func FakeUserCurrent(mock func() (*user.User, error)) func() {
	realUserCurrent := userCurrent
	userCurrent = mock

	return func() { userCurrent = realUserCurrent }
}

func FakeSudoersDotD(mockDir string) func() {
	realSudoersD := sudoersDotD
	sudoersDotD = mockDir

	return func() { sudoersDotD = realSudoersD }
}

func FakeSyscallKill(f func(int, syscall.Signal) error) func() {
	oldSyscallKill := syscallKill
	syscallKill = f
	return func() {
		syscallKill = oldSyscallKill
	}
}

func FakeSyscallStatfs(f func(string, *syscall.Statfs_t) error) func() {
	oldSyscallStatfs := syscallStatfs
	syscallStatfs = f
	return func() {
		syscallStatfs = oldSyscallStatfs
	}
}

func FakeSyscallGetpgid(f func(int) (int, error)) func() {
	oldSyscallGetpgid := syscallGetpgid
	syscallGetpgid = f
	return func() {
		syscallGetpgid = oldSyscallGetpgid
	}
}

func FakeCmdWaitTimeout(timeout time.Duration) func() {
	oldCmdWaitTimeout := cmdWaitTimeout
	cmdWaitTimeout = timeout
	return func() {
		cmdWaitTimeout = oldCmdWaitTimeout
	}
}

func WaitingReaderGuts(r io.Reader) (io.Reader, *exec.Cmd) {
	wr := r.(*waitingReader)
	return wr.reader, wr.cmd
}

func FakeChown(f func(*os.File, sys.UserID, sys.GroupID) error) func() {
	oldChown := chown
	chown = f
	return func() {
		chown = oldChown
	}
}

func FakeLookPath(new func(string) (string, error)) (restore func()) {
	old := lookPath
	lookPath = new
	return func() {
		lookPath = old
	}
}

func MockhasAddUserExecutable(new func() bool) (restore func()) {
	old := hasAddUserExecutable
	hasAddUserExecutable = new
	return func() {
		hasAddUserExecutable = old
	}
}

func SetAtomicFileRenamed(aw *AtomicFile, renamed bool) {
	aw.renamed = renamed
}

func SetUnsafeIO(b bool) func() {
	oldSnapdUnsafeIO := snapdUnsafeIO
	snapdUnsafeIO = b
	return func() {
		snapdUnsafeIO = oldSnapdUnsafeIO
	}
}

func GetUnsafeIO() bool {
	// a getter so that tests do not attempt to modify that directly
	return snapdUnsafeIO
}

func FakeOsReadlink(f func(string) (string, error)) func() {
	realOsReadlink := osReadlink
	osReadlink = f

	return func() { osReadlink = realOsReadlink }
}

// FakeEtcFstab mocks content of /etc/fstab read by IsHomeUsingNFS
func FakeEtcFstab(text string) (restore func()) {
	old := etcFstab
	f, err := os.CreateTemp("", "fstab")
	if err != nil {
		panic(fmt.Errorf("cannot open temporary file: %s", err))
	}
	if err := os.WriteFile(f.Name(), []byte(text), 0644); err != nil {
		panic(fmt.Errorf("cannot write mock fstab file: %s", err))
	}
	etcFstab = f.Name()
	return func() {
		if etcFstab == "/etc/fstab" {
			panic("respectfully refusing to remove /etc/fstab")
		}
		os.Remove(etcFstab)
		etcFstab = old
	}
}

// FakeUname mocks syscall.Uname as used by MachineName and KernelVersion
func FakeUname(f func(*syscall.Utsname) error) (restore func()) {
	r := testutil.Backup(&syscallUname)
	syscallUname = f
	return r
}

var (
	FindUidNoGetentFallback = findUidNoGetentFallback
	FindGidNoGetentFallback = findGidNoGetentFallback

	FindUidWithGetentFallback = findUidWithGetentFallback
	FindGidWithGetentFallback = findGidWithGetentFallback
)

func FakeFindUidNoFallback(mock func(name string) (uint64, error)) (restore func()) {
	old := findUidNoGetentFallback
	findUidNoGetentFallback = mock
	return func() { findUidNoGetentFallback = old }
}

func FakeFindGidNoFallback(mock func(name string) (uint64, error)) (restore func()) {
	old := findGidNoGetentFallback
	findGidNoGetentFallback = mock
	return func() { findGidNoGetentFallback = old }
}

var ParseRawEnvironment = parseRawEnvironment

// ParseRawExpandableEnv returns a new expandable environment parsed from key=value strings.
func ParseRawExpandableEnv(entries []string) (ExpandableEnv, error) {
	om := strutil.NewOrderedMap()
	for _, entry := range entries {
		key, value, err := parseEnvEntry(entry)
		if err != nil {
			return ExpandableEnv{}, err
		}
		if om.Get(key) != "" {
			return ExpandableEnv{}, fmt.Errorf("cannot overwrite earlier value of %q", key)
		}
		om.Set(key, value)
	}
	return ExpandableEnv{OrderedMap: om}, nil
}

func ReadGoBuildID(fname string) (string, error) {
	return readGenericBuildID(fname, goElfNote, goHdrType)
}

func FakeAllDataHomeGlobs(f func() []string) func() {
	oldAllDataHomeGlobs := dirsAllDataHomeGlobs
	dirsAllDataHomeGlobs = f
	return func() {
		dirsAllDataHomeGlobs = oldAllDataHomeGlobs
	}
}

func FakeFChmod(f func(file *os.File, mode os.FileMode) error) (restore func()) {
	return testutil.Fake(&fChmod, f)
}
