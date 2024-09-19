//go:build roottest

// Copyright (c) 2024 Canonical Ltd
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
	"fmt"
	"log"
	"os"
	"os/user"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/canonical/pebble/client"
	"github.com/canonical/pebble/internals/reaper"
)

var rootTestDaemon *Daemon
var rootTestPebbleClient *client.Client

func TestMain(m *testing.M) {
	err := reaper.Start()
	if err != nil {
		fmt.Printf("cannot start reaper: %v", err)
		os.Exit(1)
	}
	tmpDir, err := os.MkdirTemp("", "pebble")
	if err != nil {
		fmt.Printf("cannot create temporary directory: %v", err)
		os.Exit(1)
	}
	socketPath := tmpDir + ".pebble.socket"
	rootTestDaemon, err := New(&Options{
		Dir:        tmpDir,
		SocketPath: socketPath,
	})
	if err != nil {
		fmt.Printf("cannot create daemon: %v", err)
		os.Exit(1)
	}
	err = rootTestDaemon.Init()
	if err != nil {
		fmt.Printf("cannot init daemon: %v", err)
		os.Exit(1)
	}
	rootTestDaemon.Start()
	rootTestPebbleClient, err = client.New(&client.Config{Socket: socketPath})
	if err != nil {
		fmt.Printf("cannot create client: %v", err)
		os.Exit(1)
	}

	exitCode := m.Run()

	err = rootTestDaemon.Stop(nil)
	if err != nil {
		fmt.Printf("cannot stop daemon: %v", err)
		os.Exit(1)
	}
	err = reaper.Stop()
	if err != nil {
		fmt.Printf("cannot stop reaper: %v", err)
		os.Exit(1)
	}
	err = os.RemoveAll(tmpDir)
	if err != nil {
		log.Fatalf("cannot remove temporary directory: %v", err)
	}

	os.Exit(exitCode)
}

func TestWithRootUserGroup(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("requires running as root")
	}
	username := os.Getenv("PEBBLE_TEST_USER")
	group := os.Getenv("PEBBLE_TEST_GROUP")
	if username == "" || group == "" {
		t.Fatalf("must set PEBBLE_TEST_USER and PEBBLE_TEST_GROUP")
	}
	stdout, stderr := pebbleExec(t, "", &client.ExecOptions{
		Command: []string{"/bin/sh", "-c", "id -n -u && id -n -g"},
		User:    username,
		Group:   group,
	})
	expectedStdout := username + "\n" + group + "\n"
	if stdout != expectedStdout {
		t.Fatalf("pebble exec stdout error, expected: %v, got %v", expectedStdout, stdout)
	}
	if stderr != "" {
		t.Fatalf("pebble exec stderr is not empty: %v", stderr)
	}

	_, err := rootTestPebbleClient.Exec(&client.ExecOptions{
		Command:     []string{"pwd"},
		Environment: map[string]string{"HOME": "/non/existent"},
		User:        username,
		Group:       group,
	})
	// c.Assert(err, ErrorMatches, `.*home directory.*does not exist`)
	if matched, _ := regexp.MatchString(`.*home directory.*does not exist`, err.Error()); !matched {
		t.Errorf("Error message doesn't match, expected: %v, got: %v", `.*home directory.*does not exist`, err.Error())
	}
}

func TestWithRootUserIDGroupID(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("requires running as root")
	}
	username := os.Getenv("PEBBLE_TEST_USER")
	group := os.Getenv("PEBBLE_TEST_GROUP")
	if username == "" || group == "" {
		t.Fatalf("must set PEBBLE_TEST_USER and PEBBLE_TEST_GROUP")
	}
	u, err := user.Lookup(username)
	if err != nil {
		t.Fatalf("cannot look up username: %v", err)
	}
	g, err := user.LookupGroup(group)
	if err != nil {
		t.Fatalf("cannot look up group: %v", err)
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		t.Fatalf("cannot convert uid to int: %v", err)
	}
	gid, err := strconv.Atoi(g.Gid)
	if err != nil {
		t.Fatalf("cannot convert gid to int: %v", err)
	}
	stdout, stderr := pebbleExec(t, "", &client.ExecOptions{
		Command: []string{"/bin/sh", "-c", "id -n -u && id -n -g"},
		UserID:  &uid,
		GroupID: &gid,
	})
	expectedStdout := username + "\n" + group + "\n"
	if stdout != expectedStdout {
		t.Fatalf("pebble exec stdout error, expected: %v, got %v", expectedStdout, stdout)
	}
	if stderr != "" {
		t.Fatalf("pebble exec stderr is not empty: %v", stderr)
	}
}

func pebbleExec(t *testing.T, stdin string, opts *client.ExecOptions) (stdout, stderr string) {
	t.Helper()

	outBuf := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	opts.Stdin = strings.NewReader(stdin)
	opts.Stdout = outBuf
	opts.Stderr = errBuf
	process, err := rootTestPebbleClient.Exec(opts)
	if err != nil {
		t.Fatalf("pebble exec failed: %v", err)
	}

	if waitErr := process.Wait(); waitErr != nil {
		t.Fatalf("pebble exec process wait error: %v", waitErr)
	}
	return outBuf.String(), errBuf.String()
}
