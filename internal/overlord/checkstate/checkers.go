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

package checkstate

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"strconv"
	"syscall"

	"github.com/canonical/pebble/internal/logger"
	"github.com/canonical/pebble/internal/osutil"
	"github.com/canonical/pebble/internal/strutil/shlex"
)

// httpChecker is a checker that ensures an HTTP GET at a specified URL returns 20x.
type httpChecker struct {
	name    string
	url     string
	headers map[string]string
}

func (c *httpChecker) check(ctx context.Context) error {
	logger.Debugf("Check %q (HTTP): requesting %q", c.name, c.url)
	client := &http.Client{}
	request, err := http.NewRequestWithContext(ctx, "GET", c.url, nil)
	for k, v := range c.headers {
		request.Header.Set(k, v)
	}

	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode > 299 {
		return fmt.Errorf("received non-20x status code %d", response.StatusCode)
	}
	return nil
}

// tcpChecker is a checker that ensures a TCP port is open.
type tcpChecker struct {
	name string
	host string
	port int
}

func (c *tcpChecker) check(ctx context.Context) error {
	logger.Debugf("Check %q (TCP): opening port %d", c.name, c.port)

	host := c.host
	if host == "" {
		host = "localhost"
	}

	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(c.port)))
	if err != nil {
		return err
	}
	err = conn.Close()
	if err != nil {
		logger.Noticef("Check %q (TCP): unexpected error closing connection: %v", c.name, err)
	}
	return nil
}

// execChecker is a checker that ensures a command executes succesfully.
type execChecker struct {
	name        string
	command     string
	environment map[string]string
	userID      *int
	user        string
	groupID     *int
	group       string
	workingDir  string
}

func (c *execChecker) check(ctx context.Context) error {
	logger.Debugf("Check %q (Exec): running %q", c.name, c.command)

	args, err := shlex.Split(c.command)
	if err != nil {
		return fmt.Errorf("cannot parse check command: %v", err)
	}

	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	for k, v := range c.environment {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	cmd.Dir = c.workingDir

	// Start as another user if specified in the check config.
	uid, gid, err := osutil.NormalizeUidGid(c.userID, c.groupID, c.user, c.group)
	if err != nil {
		return err
	}
	if uid != nil && gid != nil {
		cmd.SysProcAttr.Credential = &syscall.Credential{
			Uid: uint32(*uid),
			Gid: uint32(*gid),
		}
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			err = fmt.Errorf("exec check timed out")
		}
		if len(output) > 0 {
			const maxLength = 1024
			if len(output) > maxLength {
				output = output[:maxLength]
				output = append(output, "..."...)
			}
		}
		return &outputError{error: err, out: string(output)}
	}
	return nil
}

type outputError struct {
	error
	out string
}

func (e *outputError) output() string {
	return e.out
}
