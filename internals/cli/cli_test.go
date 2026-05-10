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

	"github.com/canonical/tc"
	"golang.org/x/term"

	"github.com/canonical/pebble/cmd"
	"github.com/canonical/pebble/internals/cli"
	"github.com/canonical/pebble/internals/overlord/pairingstate"
	"github.com/canonical/pebble/internals/plan"
	"github.com/canonical/pebble/internals/workloads"
)

type BasePebbleSuite struct {
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

func (s *BasePebbleSuite) SetUpTest(c *tc.C) {
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

	c.Cleanup(cli.FakeIsStdoutTTY(false))
	c.Cleanup(cli.FakeIsStdinTTY(false))

	oldConfigHome := os.Getenv("XDG_CONFIG_HOME")
	c.Cleanup(func() {
		os.Setenv("XDG_CONFIG_HOME", oldConfigHome)
	})
	configHome := c.MkDir()
	os.Setenv("XDG_CONFIG_HOME", configHome)
	s.cliStatePath = filepath.Join(configHome, "pebble", "cli.json")

	c.Cleanup(func() {
		s.pebbleDir = ""
		s.stdin = nil
		s.stdout = nil
		s.stderr = nil
		s.password = ""
		s.cliStatePath = ""
	})
}

func (s *BasePebbleSuite) TearDownTest(c *tc.C) {
	os.Setenv("PEBBLE", "")
	os.Setenv("PEBBLE_SOCKET", "")

	cli.Stdin = os.Stdin
	cli.Stdout = os.Stdout
	cli.Stderr = os.Stderr
	cli.ReadPassword = term.ReadPassword

	plan.UnregisterSectionExtension(workloads.WorkloadsField)
	plan.UnregisterSectionExtension(pairingstate.PairingField)
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

func (s *BasePebbleSuite) RedirectClientToTestServer(c *tc.C, handler func(http.ResponseWriter, *http.Request)) {
	server := httptest.NewServer(http.HandlerFunc(handler))
	c.Cleanup(func() { server.Close() })
	c.Cleanup(cli.FakeClientConfigBaseURL(server.URL))
}

// DecodedRequestBody returns the JSON-decoded body of the request.
func DecodedRequestBody(c *tc.C, r *http.Request) map[string]any {
	var body map[string]any
	decoder := json.NewDecoder(r.Body)
	decoder.UseNumber()
	err := decoder.Decode(&body)
	c.Assert(err, tc.ErrorIsNil)
	return body
}

// EncodeResponseBody writes JSON-serialized body to the response writer.
func EncodeResponseBody(c *tc.C, w http.ResponseWriter, body any) {
	encoder := json.NewEncoder(w)
	err := encoder.Encode(body)
	c.Assert(err, tc.ErrorIsNil)
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

func TestPebbleSuite(t *testing.T) {
	tc.Run(t, &PebbleSuite{})
}

func (s *PebbleSuite) TestErrorResult(c *tc.C) {
	s.RedirectClientToTestServer(c, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"type": "error", "result": {"message": "cannot do something"}}`)
	})

	restore := fakeArgs("pebble", "notices")
	defer restore()

	err := cli.RunMain()
	c.Assert(err, tc.ErrorMatches, `cannot do something`)
}

func (s *PebbleSuite) TestRunOptionApplyDefaults(c *tc.C) {
	os.Setenv("PEBBLE", "")
	os.Setenv("PEBBLE_SOCKET", "")
	r := cli.WithDefaultRunOptions(nil)
	c.Assert(r.PebbleDir, tc.Equals, "/var/lib/pebble/default")
	c.Assert(r.ClientConfig.Socket, tc.Equals, "/var/lib/pebble/default/.pebble.socket")

	os.Setenv("PEBBLE", "/foo")
	r = cli.WithDefaultRunOptions(nil)
	c.Assert(r.PebbleDir, tc.Equals, "/foo")
	c.Assert(r.ClientConfig.Socket, tc.Equals, "/foo/.pebble.socket")

	os.Setenv("PEBBLE", "/bar")
	os.Setenv("PEBBLE_SOCKET", "/path/to/socket")
	r = cli.WithDefaultRunOptions(nil)
	c.Assert(r.PebbleDir, tc.Equals, "/bar")
	c.Assert(r.ClientConfig.Socket, tc.Equals, "/path/to/socket")
}

func (s *BasePebbleSuite) readCLIState(c *tc.C) map[string]any {
	data, err := os.ReadFile(s.cliStatePath)
	c.Assert(err, tc.ErrorIsNil)
	var fullState map[string]any
	err = json.Unmarshal(data, &fullState)
	c.Assert(err, tc.ErrorIsNil)

	socketMap, ok := fullState["pebble"].(map[string]any)
	if !ok {
		c.Fatalf("expected socket map, got %#v", fullState["pebble"])
	}

	r := cli.WithDefaultRunOptions(nil)
	v, ok := socketMap[r.ClientConfig.Socket]
	if !ok {
		c.Fatalf("expected state map, got %#v", socketMap[r.ClientConfig.Socket])
	}
	return v.(map[string]any)
}

func (s *BasePebbleSuite) writeCLIState(c *tc.C, st map[string]any) {
	r := cli.WithDefaultRunOptions(nil)
	fullState := map[string]any{
		"pebble": map[string]any{
			r.ClientConfig.Socket: st,
		},
	}
	err := os.MkdirAll(filepath.Dir(s.cliStatePath), 0o700)
	c.Assert(err, tc.ErrorIsNil)
	data, err := json.Marshal(fullState)
	c.Assert(err, tc.ErrorIsNil)
	err = os.WriteFile(s.cliStatePath, data, 0o600)
	c.Assert(err, tc.ErrorIsNil)
}
