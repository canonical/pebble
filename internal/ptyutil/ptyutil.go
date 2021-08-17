// Copyright (c) 2021 Canonical Ltd
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

package ptyutil

import (
	"fmt"
	"os"
	"unsafe"

	"github.com/pkg/term/termios"
	"golang.org/x/sys/unix"
)

// OpenPtyInDevpts creates a new PTS pair, configures them and returns them.
func OpenPtyInDevpts(devpts_fd int, uid, gid int64) (*os.File, *os.File, error) {
	revert := true
	var ptx *os.File
	var err error

	// Create a PTS pair.
	if devpts_fd >= 0 {
		fd, err := unix.Openat(devpts_fd, "ptmx", os.O_RDWR|unix.O_CLOEXEC, 0)
		if err == nil {
			ptx = os.NewFile(uintptr(fd), "/dev/pts/ptmx")
		}
	} else {
		ptx, err = os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_CLOEXEC, 0)
		if err != nil {
			return nil, nil, err
		}
	}
	defer func() {
		if revert {
			ptx.Close()
		}
	}()

	// Unlock the ptx and pty.
	val := 0
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(ptx.Fd()), unix.TIOCSPTLCK, uintptr(unsafe.Pointer(&val)))
	if errno != 0 {
		return nil, nil, unix.Errno(errno)
	}

	var pty *os.File
	ptyFd, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(ptx.Fd()), unix.TIOCGPTPEER, uintptr(unix.O_NOCTTY|unix.O_CLOEXEC|os.O_RDWR))
	// We can only fallback to looking up the fd in /dev/pts when we aren't dealing with the container's devpts instance.
	if errno == 0 {
		// Get the pty side.
		id := 0
		_, _, errno = unix.Syscall(unix.SYS_IOCTL, uintptr(ptx.Fd()), unix.TIOCGPTN, uintptr(unsafe.Pointer(&id)))
		if errno != 0 {
			return nil, nil, unix.Errno(errno)
		}

		pty = os.NewFile(ptyFd, fmt.Sprintf("/dev/pts/%d", id))
	} else {
		if devpts_fd >= 0 {
			return nil, nil, fmt.Errorf("TIOCGPTPEER required but not available")
		}

		// Get the pty side.
		id := 0
		_, _, errno = unix.Syscall(unix.SYS_IOCTL, uintptr(ptx.Fd()), unix.TIOCGPTN, uintptr(unsafe.Pointer(&id)))
		if errno != 0 {
			return nil, nil, unix.Errno(errno)
		}

		// Open the pty.
		pty, err = os.OpenFile(fmt.Sprintf("/dev/pts/%d", id), os.O_RDWR|unix.O_NOCTTY, 0)
		if err != nil {
			return nil, nil, err
		}
	}
	defer func() {
		if revert {
			pty.Close()
		}
	}()

	// Configure both sides
	for _, entry := range []*os.File{ptx, pty} {
		// Get termios.
		t, err := unix.IoctlGetTermios(int(entry.Fd()), unix.TCGETS)
		if err != nil {
			return nil, nil, err
		}

		// Set flags.
		t.Cflag |= unix.IMAXBEL
		t.Cflag |= unix.IUTF8
		t.Cflag |= unix.BRKINT
		t.Cflag |= unix.IXANY
		t.Cflag |= unix.HUPCL

		// Set termios.
		err = unix.IoctlSetTermios(int(entry.Fd()), unix.TCSETS, t)
		if err != nil {
			return nil, nil, err
		}

		// Set the default window size.
		sz := &unix.Winsize{
			Col: 80,
			Row: 25,
		}

		err = unix.IoctlSetWinsize(int(entry.Fd()), unix.TIOCSWINSZ, sz)
		if err != nil {
			return nil, nil, err
		}

		// Set CLOEXEC.
		_, _, errno = unix.Syscall(unix.SYS_FCNTL, uintptr(entry.Fd()), unix.F_SETFD, unix.FD_CLOEXEC)
		if errno != 0 {
			return nil, nil, unix.Errno(errno)
		}
	}

	// Fix the ownership of the pty side.
	err = unix.Fchown(int(pty.Fd()), int(uid), int(gid))
	if err != nil {
		return nil, nil, err
	}

	revert = false
	return ptx, pty, nil
}

// OpenPty creates a new PTS pair, configures them and returns them.
func OpenPty(uid, gid int64) (*os.File, *os.File, error) {
	return OpenPtyInDevpts(-1, uid, gid)
}

// SetSize sets the dimensions of the terminal associated with fd.
func SetSize(fd int, width int, height int) (err error) {
	var dimensions [4]uint16
	dimensions[0] = uint16(height)
	dimensions[1] = uint16(width)

	if _, _, err := unix.Syscall6(unix.SYS_IOCTL, uintptr(fd), uintptr(unix.TIOCSWINSZ), uintptr(unsafe.Pointer(&dimensions)), 0, 0, 0); err != 0 {
		return err
	}
	return nil
}

// GetSize returns the dimensions of the given terminal.
func GetSize(fd int) (int, int, error) {
	winsize, err := unix.IoctlGetWinsize(fd, unix.TIOCGWINSZ)
	if err != nil {
		return -1, -1, err
	}

	return int(winsize.Col), int(winsize.Row), nil
}

// State contains the state of a terminal.
type State struct {
	Termios unix.Termios
}

// IsTerminal returns true if the given file descriptor is a terminal.
func IsTerminal(fd int) bool {
	_, err := GetState(fd)
	return err == nil
}

// GetState returns the current state of a terminal which may be useful to restore the terminal after a signal.
func GetState(fd int) (*State, error) {
	termios, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		return nil, err
	}

	state := State{}
	state.Termios = *termios

	return &state, nil
}

// MakeRaw put the terminal connected to the given file descriptor into raw mode and returns the previous state of the terminal so that it can be restored.
func MakeRaw(fd int) (*State, error) {
	var err error
	var oldState, newState *State

	oldState, err = GetState(fd)
	if err != nil {
		return nil, err
	}

	newState = &State{}
	newState.Termios = oldState.Termios

	termios.Cfmakeraw(&newState.Termios)

	err = Restore(fd, newState)
	if err != nil {
		return nil, err
	}

	return oldState, nil
}

// Restore restores the terminal connected to the given file descriptor to a previous state.
func Restore(fd int, state *State) error {
	return termios.Tcsetattr(uintptr(fd), termios.TCSANOW, &state.Termios)
}
