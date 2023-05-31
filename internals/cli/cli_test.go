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
	stdin     *bytes.Buffer
	stdout    *bytes.Buffer
	stderr    *bytes.Buffer
	password  string
	pebbleDir string

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
	os.Setenv("PEBBLE", "")
	os.Setenv("PEBBLE_SOCKET", "")
	pebbleDir, socketPath := cli.GetEnvPaths()
	c.Assert(pebbleDir, Equals, "/var/lib/pebble/default")
	c.Assert(socketPath, Equals, "/var/lib/pebble/default/.pebble.socket")

	os.Setenv("PEBBLE", "/foo")
	pebbleDir, socketPath = cli.GetEnvPaths()
	c.Assert(pebbleDir, Equals, "/foo")
	c.Assert(socketPath, Equals, "/foo/.pebble.socket")

	os.Setenv("PEBBLE", "/bar")
	os.Setenv("PEBBLE_SOCKET", "/path/to/socket")
	pebbleDir, socketPath = cli.GetEnvPaths()
	c.Assert(pebbleDir, Equals, "/bar")
	c.Assert(socketPath, Equals, "/path/to/socket")
}
