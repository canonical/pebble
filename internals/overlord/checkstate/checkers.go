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
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/canonical/x-go/strutil/shlex"

	"github.com/canonical/pebble/internals/logger"
	"github.com/canonical/pebble/internals/osutil"
	"github.com/canonical/pebble/internals/reaper"
	"github.com/canonical/pebble/internals/servicelog"
)

const (
	maxErrorBytes = 512
	maxErrorLines = 5
	execWaitDelay = time.Second
)

// httpChecker is a checker that ensures an HTTP GET at a specified URL returns 2xx.
type httpChecker struct {
	name         string
	url          string
	headers      map[string]string
	trustContext string
	trustMgr     TrustManager
}

func (c *httpChecker) check(ctx context.Context) error {
	logger.Debugf("Check %q (http): requesting %q", c.name, c.url)
	client := &http.Client{}

	// Resolve the trust context configured for this check (falling back to
	// the "default" trust context if none is set), and use its CA bundle to
	// validate the server's certificate, unless it resolves to the system CA
	// pool, in which case the default HTTP client behaviour is used.
	if c.trustMgr == nil {
		logger.Noticef("Check %q (exec): cannot resolve trust context %q: no trust manager", c.name, c.trustContext)
	} else if trustContext, err := c.trustMgr.TrustContext(c.trustContext); err != nil {
		logger.Noticef("Check %q (exec): cannot resolve trust context %q: %v", c.name, c.trustContext, err)
	} else if trustContext.IsSystemCA() {
		trustContext.Close()
	} else {
		defer trustContext.Close()
		pool, err := trustContext.CAPool()
		if err != nil {
			logger.Noticef("Check %q (http): cannot get CA pool for trust context %q: %v", c.name, c.trustContext, err)
		} else {
			client.Transport = &http.Transport{
				TLSClientConfig: &tls.Config{RootCAs: pool},
			}
		}
	}

	request, err := http.NewRequestWithContext(ctx, "GET", c.url, nil)
	if err != nil {
		return fmt.Errorf("cannot build request: %w", err)
	}
	for k, v := range c.headers {
		request.Header.Set(k, v)
	}

	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode > 299 {
		// Include first few lines of response body in error details
		output, err := io.ReadAll(io.LimitReader(response.Body, maxErrorBytes))
		details := ""
		if err != nil {
			details = fmt.Sprintf("cannot read response: %v", err)
		} else {
			lines := strings.Split(strings.TrimSpace(string(output)), "\n")
			if len(lines) > maxErrorLines {
				lines = lines[:maxErrorLines+1]
				lines[maxErrorLines] = "(...)"
			}
			details = strings.Join(lines, "\n")
		}
		return &detailsError{
			error:   fmt.Errorf("non-2xx status code %d", response.StatusCode),
			details: details,
		}
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
	logger.Debugf("Check %q (tcp): opening port %d", c.name, c.port)

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
		logger.Noticef("Check %q (tcp): unexpected error closing connection: %v", c.name, err)
	}
	return nil
}

// execChecker is a checker that ensures a command executes successfully.
type execChecker struct {
	name         string
	command      string
	environment  map[string]string
	userID       *int
	user         string
	groupID      *int
	group        string
	workingDir   string
	trustContext string
	trustMgr     TrustManager
}

func (c *execChecker) check(ctx context.Context) error {
	args, err := shlex.Split(c.command)
	if err != nil {
		return fmt.Errorf("cannot parse command: %v", err)
	}

	// Similar to services and exec, inherit the daemon's environment.
	environment := osutil.Environ()
	// Requested environment takes precedence.
	maps.Copy(environment, c.environment)

	// Resolve the trust context configured for this check (falling back to
	// the "default" trust context if none is set), and provide its CA bundle
	// via SSL_CERT_FILE, unless the check has set that environment variable
	// itself, or the trust context resolves to the system CA pool.
	if c.trustMgr == nil {
		logger.Noticef("Check %q (exec): cannot resolve trust context %q: no trust manager", c.name, c.trustContext)
	} else if trustContext, err := c.trustMgr.TrustContext(c.trustContext); err != nil {
		logger.Noticef("Check %q (exec): cannot resolve trust context %q: %v", c.name, c.trustContext, err)
	} else if trustContext.IsSystemCA() || c.environment["SSL_CERT_FILE"] != "" {
		trustContext.Close()
	} else {
		defer trustContext.Close()
		caBundleFile, err := trustContext.CABundleFile()
		if err != nil {
			logger.Noticef(
				"Check %q (exec): cannot get CA bundle file for trust context %q: %v",
				c.name, c.trustContext, err,
			)
		} else {
			environment["SSL_CERT_FILE"] = caBundleFile
		}
	}

	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Env = make([]string, 0, len(environment)) // avoid additional allocations
	for k, v := range environment {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	cmd.Dir = c.workingDir

	// Start as another user if specified in the check config.
	uid, gid, err := osutil.NormalizeUidGid(c.userID, c.groupID, c.user, c.group)
	if err != nil {
		return err
	}
	if uid != nil && gid != nil {
		isCurrent, err := osutil.IsCurrent(*uid, *gid)
		if err != nil {
			logger.Debugf("Cannot determine if uid %d gid %d is current user", *uid, *gid)
		}
		if !isCurrent {
			cmd.SysProcAttr = &syscall.SysProcAttr{}
			cmd.SysProcAttr.Credential = &syscall.Credential{
				Uid: uint32(*uid),
				Gid: uint32(*gid),
			}
		}
	}

	// Start service, sending output to a ring buffer so we can show the last
	// few lines of output on error.
	ringBuffer := servicelog.NewRingBuffer(maxErrorBytes)
	defer ringBuffer.Close()
	cmd.Stdout = ringBuffer
	cmd.Stderr = ringBuffer
	cmd.WaitDelay = execWaitDelay
	err = reaper.StartCommand(cmd)
	if err != nil {
		return err
	}
	logger.Debugf("Check %q (exec): running %q (PID %d)", c.name, c.command, cmd.Process.Pid)

	exitCode, err := reaper.WaitCommand(cmd)
	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		// If context is cancelled or times out, exitCode will be 137
		// and err will be nil, so return the ctx.Err() directly.
		return ctx.Err()
	}
	if err == nil && exitCode > 0 {
		err = fmt.Errorf("exit status %d", exitCode)
	}
	if err != nil {
		// Include the last few lines of output in the error details
		var details string
		details, linesErr := servicelog.LastLines(ringBuffer, maxErrorLines, "", false)
		if linesErr != nil {
			details = fmt.Sprintf("cannot read output: %v", linesErr)
		}
		return &detailsError{error: err, details: details}
	}
	return nil
}

type detailsError struct {
	error
	details string
}

func (e *detailsError) Details() string {
	return e.details
}

func (e *detailsError) Unwrap() error {
	return e.error
}
