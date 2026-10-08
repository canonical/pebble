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
	"math"
	"unsafe"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// GetTermios returns the terminal attributes of the terminal associated with fd.
func GetTermios(fd int) (*unix.Termios, error) {
	return unix.IoctlGetTermios(fd, ioctlGetTermios)
}

// SetSize sets the dimensions of the terminal associated with fd.
func SetSize(fd int, width int, height int) (err error) {
	if width < 0 || width > math.MaxUint16 || height < 0 || height > math.MaxUint16 {
		return fmt.Errorf("cannot set terminal size: dimension out of range")
	}

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

// State is an alias for the terminal state returned by x/term.
type State = term.State

// IsTerminal returns true if the given file descriptor is a terminal.
func IsTerminal(fd int) bool {
	return term.IsTerminal(fd)
}

// GetState returns the current state of a terminal, which may be useful to restore the terminal after a signal.
func GetState(fd int) (*State, error) {
	return term.GetState(fd)
}

// MakeRaw puts the terminal connected to the given file descriptor into raw mode and returns the previous state of the terminal so that it can be restored.
func MakeRaw(fd int) (*State, error) {
	return term.MakeRaw(fd)
}

// Restore restores the terminal connected to the given file descriptor to a previous state.
func Restore(fd int, state *State) error {
	return term.Restore(fd, state)
}
