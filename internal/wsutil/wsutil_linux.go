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

package wsutil

import (
	"io"
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/gorilla/websocket"
	"golang.org/x/sys/unix"

	"github.com/canonical/pebble/internal/logger"
)

// MirrorToWebsocket mirrors PTY output from r (file descriptor fd) to the websocket.
func MirrorToWebsocket(conn MessageWriter, r io.ReadCloser, exited chan struct{}, fd int) {
	in := ExecReaderToChannel(r, -1, exited, fd)
	for {
		buf, ok := <-in
		if !ok {
			r.Close()
			logger.Debugf("Sending write barrier")
			err := conn.WriteMessage(websocket.TextMessage, endCommandJSON)
			if err != nil {
				logger.Debugf("Got err writing barrier %s", err)
			}
			return
		}

		err := conn.WriteMessage(websocket.BinaryMessage, buf)
		if err != nil {
			logger.Debugf("Got err writing %s", err)
			break
		}
	}

	closeMsg := websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")
	conn.WriteMessage(websocket.CloseMessage, closeMsg)
	r.Close()
}

// Extensively commented directly in the code. Please leave the comments!
// Looking at this in a couple of months noone will know why and how this works
// anymore.
func ExecReaderToChannel(r io.Reader, bufferSize int, exited <-chan struct{}, fd int) <-chan []byte {
	if bufferSize <= (128 * 1024) {
		bufferSize = (128 * 1024)
	}

	ch := make(chan ([]byte))

	// Takes care that the closeChannel() function is exactly executed once.
	// This allows us to avoid using a mutex.
	var once sync.Once
	closeChannel := func() {
		close(ch)
	}

	// [1]: This function has just one job: Dealing with the case where we
	// are running an interactive shell session where we put a process in
	// the background that does hold stdin/stdout open, but does not
	// generate any output at all. This case cannot be dealt with in the
	// following function call. Here's why: Assume the above case, now the
	// attached child (the shell in this example) exits. This will not
	// generate any poll() event: We won't get POLLHUP because the
	// background process is holding stdin/stdout open and noone is writing
	// to it. So we effectively block on GetPollRevents() in the function
	// below. Hence, we use another go routine here who's only job is to
	// handle that case: When we detect that the child has exited we check
	// whether a POLLIN or POLLHUP event has been generated. If not, we know
	// that there's nothing buffered on stdout and exit.
	var attachedChildIsDead int32 = 0
	go func() {
		<-exited

		atomic.StoreInt32(&attachedChildIsDead, 1)

		ret, revents, err := GetPollRevents(fd, 0, (unix.POLLIN | unix.POLLPRI | unix.POLLERR | unix.POLLHUP | unix.POLLRDHUP | unix.POLLNVAL))
		if ret < 0 {
			logger.Noticef("Failed to poll(POLLIN | POLLPRI | POLLHUP | POLLRDHUP) on file descriptor: %s.", err)
			// Something went wrong so let's exited otherwise we
			// end up in an endless loop.
			once.Do(closeChannel)
		} else if ret > 0 {
			if (revents & unix.POLLERR) > 0 {
				logger.Noticef("Detected poll(POLLERR) event.")
				// Read end has likely been closed so again,
				// avoid an endless loop.
				once.Do(closeChannel)
			} else if (revents & unix.POLLNVAL) > 0 {
				logger.Debugf("Detected poll(POLLNVAL) event.")
				// Well, someone closed the fd havent they? So
				// let's go home.
				once.Do(closeChannel)
			}
		} else if ret == 0 {
			logger.Debugf("No data in stdout: exiting.")
			once.Do(closeChannel)
		}
	}()

	go func() {
		readSize := (128 * 1024)
		offset := 0
		buf := make([]byte, bufferSize)
		avoidAtomicLoad := false

		defer once.Do(closeChannel)
		for {
			nr := 0
			var err error

			ret, revents, err := GetPollRevents(fd, -1, (unix.POLLIN | unix.POLLPRI | unix.POLLERR | unix.POLLHUP | unix.POLLRDHUP | unix.POLLNVAL))
			if ret < 0 {
				// This condition is only reached in cases where we are massively f*cked since we even handle
				// EINTR in the underlying C wrapper around poll(). So let's exit here.
				logger.Noticef("Failed to poll(POLLIN | POLLPRI | POLLERR | POLLHUP | POLLRDHUP) on file descriptor: %s. Exiting.", err)
				return
			}

			// [2]: If the process exits before all its data has been read by us and no other process holds stdin or
			// stdout open, then we will observe a (POLLHUP | POLLRDHUP | POLLIN) event. This means, we need to
			// keep on reading from the pty file descriptor until we get a simple POLLHUP back.
			both := ((revents & (unix.POLLIN | unix.POLLPRI)) > 0) && ((revents & (unix.POLLHUP | unix.POLLRDHUP)) > 0)
			if both {
				logger.Debugf("Detected poll(POLLIN | POLLPRI | POLLHUP | POLLRDHUP) event.")
				read := buf[offset : offset+readSize]
				nr, err = r.Read(read)
			}

			if (revents & unix.POLLERR) > 0 {
				logger.Noticef("Detected poll(POLLERR) event: exiting.")
				return
			} else if (revents & unix.POLLNVAL) > 0 {
				logger.Noticef("Detected poll(POLLNVAL) event: exiting.")
				return
			}

			if ((revents & (unix.POLLIN | unix.POLLPRI)) > 0) && !both {
				// This might appear unintuitive at first but is actually a nice trick: Assume we are running
				// a shell session in a container and put a process in the background that is writing to
				// stdout. Now assume the attached process (aka the shell in this example) exits because we
				// used Ctrl+D to send EOF or something. If no other process would be holding stdout open we
				// would expect to observe either a (POLLHUP | POLLRDHUP | POLLIN | POLLPRI) event if there
				// is still data buffered from the previous process or a simple (POLLHUP | POLLRDHUP) if
				// no data is buffered. The fact that we only observe a (POLLIN | POLLPRI) event means that
				// another process is holding stdout open and is writing to it.
				// One counter argument that can be leveraged is (brauner looks at tycho :))
				// "Hey, you need to write at least one additional tty buffer to make sure that
				// everything that the attached child has written is actually shown."
				// The answer to that is:
				// "This case can only happen if the process has exited and has left data in stdout which
				// would generate a (POLLIN | POLLPRI | POLLHUP | POLLRDHUP) event and this case is already
				// handled and triggers another codepath. (See [2].)"
				if avoidAtomicLoad || atomic.LoadInt32(&attachedChildIsDead) == 1 {
					avoidAtomicLoad = true
					// Handle race between atomic.StoreInt32() in the go routine
					// explained in [1] and atomic.LoadInt32() in the go routine
					// here:
					// We need to check for (POLLHUP | POLLRDHUP) here again since we might
					// still be handling a pure POLLIN event from a write prior to the childs
					// exit. But the child might have exited right before and performed
					// atomic.StoreInt32() to update attachedChildIsDead before we
					// performed our atomic.LoadInt32(). This means we accidentally hit this
					// codepath and are misinformed about the available poll() events. So we
					// need to perform a non-blocking poll() again to exclude that case:
					//
					// - If we detect no (POLLHUP | POLLRDHUP) event we know the child
					//   has already exited but someone else is holding stdin/stdout open and
					//   writing to it.
					//   Note that his case should only ever be triggered in situations like
					//   running a shell and doing stuff like:
					//    > ./lxc exec xen1 -- bash
					//   root@xen1:~# yes &
					//   .
					//   .
					//   .
					//   now send Ctrl+D or type "exit". By the time the Ctrl+D/exit event is
					//   triggered, we will have read all of the childs data it has written to
					//   stdout and so we can assume that anything that comes now belongs to
					//   the process that is holding stdin/stdout open.
					//
					// - If we detect a (POLLHUP | POLLRDHUP) event we know that we've
					//   hit this codepath on accident caused by the race between
					//   atomic.StoreInt32() in the go routine explained in [1] and
					//   atomic.LoadInt32() in this go routine. So the next call to
					//   GetPollRevents() will either return
					//   (POLLIN | POLLPRI | POLLERR | POLLHUP | POLLRDHUP)
					//   or (POLLHUP | POLLRDHUP). Both will trigger another codepath (See [2].)
					//   that takes care that all data of the child that is buffered in
					//   stdout is written out.
					ret, revents, err := GetPollRevents(fd, 0, (unix.POLLIN | unix.POLLPRI | unix.POLLERR | unix.POLLHUP | unix.POLLRDHUP | unix.POLLNVAL))
					if ret < 0 {
						logger.Noticef("Failed to poll(POLLIN | POLLPRI | POLLERR | POLLHUP | POLLRDHUP) on file descriptor: %s. Exiting.", err)
						return
					} else if (revents & (unix.POLLHUP | unix.POLLRDHUP | unix.POLLERR | unix.POLLNVAL)) == 0 {
						logger.Debugf("Exiting but background processes are still running.")
						return
					}
				}
				read := buf[offset : offset+readSize]
				nr, err = r.Read(read)
			}

			// The attached process has exited and we have read all data that may have
			// been buffered.
			if ((revents & (unix.POLLHUP | unix.POLLRDHUP)) > 0) && !both {
				logger.Debugf("Detected poll(POLLHUP) event: exiting.")
				return
			}

			offset += nr
			if offset > 0 && (offset+readSize >= bufferSize || err != nil) {
				ch <- buf[0:offset]
				offset = 0
				buf = make([]byte, bufferSize)
			}
		}
	}()

	return ch
}

// GetPollRevents poll for events on provided fd.
func GetPollRevents(fd int, timeout int, flags int) (int, int, error) {
	pollFd := unix.PollFd{
		Fd:      int32(fd),
		Events:  int16(flags),
		Revents: 0,
	}
	pollFds := []unix.PollFd{pollFd}

again:
	n, err := unix.Poll(pollFds, timeout)
	if err != nil {
		if err == syscall.EAGAIN || err == syscall.EINTR {
			goto again
		}

		return -1, -1, err
	}

	return n, int(pollFds[0].Revents), err
}
