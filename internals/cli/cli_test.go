package cli_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh/terminal"
	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/cmd"
	"github.com/canonical/pebble/internals/cli"
	"github.com/canonical/pebble/internals/testutil"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type BasePebbleSuite struct {
	testutil.BaseTest
	stdin        *bytes.Buffer
	stdout       *bytes.Buffer
	stderr       *bytes.Buffer
	password     string
	pebbleDir    string
	cliStatePath string

	AuthFile string
}

func (s *BasePebbleSuite) readPassword(fd int) ([]byte, error) {
	return []byte(s.password), nil
}

func (s *BasePebbleSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.pebbleDir = c.MkDir()
	os.Setenv("PEBBLE", s.pebbleDir)

	s.stdin = bytes.NewBuffer(nil)
	s.stdout = bytes.NewBuffer(nil)
	s.stderr = bytes.NewBuffer(nil)
	s.password = ""

	cli.Stdin = s.stdin
	cli.Stdout = s.stdout
	cli.Stderr = s.stderr
	cli.ReadPassword = s.readPassword

	s.AddCleanup(cli.FakeIsStdoutTTY(false))
	s.AddCleanup(cli.FakeIsStdinTTY(false))

	os.Setenv("PEBBLE_LAST_WARNING_TIMESTAMP_FILENAME", filepath.Join(c.MkDir(), "warnings.json"))

	oldConfigHome := os.Getenv("XDG_CONFIG_HOME")
	s.AddCleanup(func() {
		os.Setenv("XDG_CONFIG_HOME", oldConfigHome)
	})
	configHome := c.MkDir()
	os.Setenv("XDG_CONFIG_HOME", configHome)
	s.cliStatePath = filepath.Join(configHome, "pebble", "cli.json")
}

func (s *BasePebbleSuite) TearDownTest(c *C) {
	os.Setenv("PEBBLE", "")
	os.Setenv("PEBBLE_SOCKET", "")

	cli.Stdin = os.Stdin
	cli.Stdout = os.Stdout
	cli.Stderr = os.Stderr
	cli.ReadPassword = terminal.ReadPassword

	os.Setenv("PEBBLE_LAST_WARNING_TIMESTAMP_FILENAME", "")

	s.BaseTest.TearDownTest(c)
}

func (s *BasePebbleSuite) Stdout() string {
	return s.stdout.String()
}

func (s *BasePebbleSuite) Stderr() string {
	return s.stderr.String()
}

func (s *BasePebbleSuite) ResetStdStreams() {
	s.stdin.Reset()
	s.stdout.Reset()
	s.stderr.Reset()
}

func (s *BasePebbleSuite) RedirectClientToTestServer(handler func(http.ResponseWriter, *http.Request)) {
	server := httptest.NewServer(http.HandlerFunc(handler))
	s.BaseTest.AddCleanup(func() { server.Close() })
	cli.ClientConfig.BaseURL = server.URL
	s.BaseTest.AddCleanup(func() { cli.ClientConfig.BaseURL = "" })
}

// DecodedRequestBody returns the JSON-decoded body of the request.
func DecodedRequestBody(c *C, r *http.Request) map[string]interface{} {
	var body map[string]interface{}
	decoder := json.NewDecoder(r.Body)
	decoder.UseNumber()
	err := decoder.Decode(&body)
	c.Assert(err, IsNil)
	return body
}

// EncodeResponseBody writes JSON-serialized body to the response writer.
func EncodeResponseBody(c *C, w http.ResponseWriter, body interface{}) {
	encoder := json.NewEncoder(w)
	err := encoder.Encode(body)
	c.Assert(err, IsNil)
}

func fakeArgs(args ...string) (restore func()) {
	old := os.Args
	os.Args = args
	return func() { os.Args = old }
}

func fakeVersion(v string) (restore func()) {
	old := cmd.Version
	cmd.Version = v
	return func() { cmd.Version = old }
}

type PebbleSuite struct {
	BasePebbleSuite
}

var _ = Suite(&PebbleSuite{})

func (s *PebbleSuite) TestErrorResult(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"type": "error", "result": {"message": "cannot do something"}}`)
	})

	restore := fakeArgs("pebble", "warnings")
	defer restore()

	err := cli.RunMain()
	c.Assert(err, ErrorMatches, `cannot do something`)
}

func (s *PebbleSuite) TestGetEnvPaths(c *C) {
	defer func() {
		os.Unsetenv("PEBBLE")
		os.Unsetenv("PEBBLE_SOCKET")
		os.Unsetenv("PEBBLE_IMPORT")
	}()

	os.Setenv("PEBBLE", "")
	os.Setenv("PEBBLE_SOCKET", "")
	os.Setenv("PEBBLE_IMPORT", "")
	paths := cli.GetEnvPaths()
	c.Assert(paths.PebbleDir, Equals, "/var/lib/pebble/default")
	c.Assert(paths.SocketPath, Equals, "/var/lib/pebble/default/.pebble.socket")
	c.Assert(paths.ImportDirs, HasLen, 0)

	os.Setenv("PEBBLE", "/foo")
	paths = cli.GetEnvPaths()
	c.Assert(paths.PebbleDir, Equals, "/foo")
	c.Assert(paths.SocketPath, Equals, "/foo/.pebble.socket")
	c.Assert(paths.ImportDirs, HasLen, 0)

	os.Setenv("PEBBLE", "/bar")
	os.Setenv("PEBBLE_SOCKET", "/path/to/socket")
	paths = cli.GetEnvPaths()
	c.Assert(paths.PebbleDir, Equals, "/bar")
	c.Assert(paths.SocketPath, Equals, "/path/to/socket")
	c.Assert(paths.ImportDirs, HasLen, 0)

	os.Setenv("PEBBLE_IMPORT", "/a:/b")
	paths = cli.GetEnvPaths()
	c.Assert(paths.ImportDirs, DeepEquals, []string{"/a", "/b"})

	os.Setenv("PEBBLE_IMPORT", "/a")
	paths = cli.GetEnvPaths()
	c.Assert(paths.ImportDirs, DeepEquals, []string{"/a"})
}

func (s *PebbleSuite) readCLIState(c *C) map[string]any {
	data, err := os.ReadFile(s.cliStatePath)
	c.Assert(err, IsNil)
	var fullState map[string]any
	err = json.Unmarshal(data, &fullState)
	c.Assert(err, IsNil)

	socketMap, ok := fullState["pebble"].(map[string]any)
	if !ok {
		c.Fatalf("expected socket map, got %#v", fullState["pebble"])
	}

	paths := cli.GetEnvPaths()
	v, ok := socketMap[paths.SocketPath]
	if !ok {
		c.Fatalf("expected state map, got %#v", socketMap[paths.SocketPath])
	}
	return v.(map[string]any)
}

func (s *PebbleSuite) writeCLIState(c *C, st map[string]any) {
	paths := cli.GetEnvPaths()
	fullState := map[string]any{
		"pebble": map[string]any{
			paths.SocketPath: st,
		},
	}
	err := os.MkdirAll(filepath.Dir(s.cliStatePath), 0o700)
	c.Assert(err, IsNil)
	data, err := json.Marshal(fullState)
	c.Assert(err, IsNil)
	err = os.WriteFile(s.cliStatePath, data, 0o600)
	c.Assert(err, IsNil)
}
