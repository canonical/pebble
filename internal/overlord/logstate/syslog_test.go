package logstate

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSyslogBackend(t *testing.T) {
	tests := []struct {
		name  string
		input string
		pid   int
		want  string
	}{
		{
			name:  "basic",
			input: "hello",
			want:  "48 <14>1 0001-01-01T00:00:00Z - testapp - - - hello",
		},
	}

	for i, test := range tests {
		t.Run(fmt.Sprintf("case %v (%v)", i+1, test.name), func(t *testing.T) {
			w, err := NewSyslogBackend("localhost:424242")
			if err != nil {
				t.Fatal(err)
			}

			// mock the internal net.Conn
			client, server := net.Pipe()
			w.conn = client

			go func() {
				err := w.Send(&LogMessage{Service: "testapp", Message: []byte(test.input), Timestamp: time.Time{}})
				if err != nil {
					t.Errorf("send failed: %v", err)
					return
				}
				client.Close()
			}()

			data, err := ioutil.ReadAll(server)
			if err != io.EOF && err != nil {
				t.Errorf("read error: %v", err)
			}

			if got := string(data); got != test.want {
				t.Errorf("wrong output:\nwant %q\ngot  %q", test.want, got)
			}
		})
	}
}

// If inputs is nil, a single sample write is generated. If wantMsgs is nil, expect each message in
// inputs to be transmitted unscathed.  If wantMsgs is not nil, it the test checks that each
// message in wantMsgs is transmitted by the transport in sequence.
func testTransport(config servConfig, inputs, wantMsgs []string) func(t *testing.T) {
	return func(t *testing.T) {
		errs := make(chan error, 20)
		// make sure this is synchronous (i.e. blocks) so that we can control the rate at which the
		// transport sends messages - allowing us to test buffer wrapping.
		msgs := make(chan string)

		addr, closer, wg := startTestServer(t, config, noCrash, msgs, errs)
		defer wg.Wait()
		defer closer.Close()

		addr = config.protocol + "://" + addr

		backend, err := NewSyslogBackend(addr)
		if err != nil {
			t.Fatal(err)
		}
		dest := NewLogDestination("testdest", backend)
		defer dest.Close()
		forwarder := NewLogForwarder(dest, "testservice")

		if len(inputs) == 0 {
			inputs = []string{"hello"}
		}
		if wantMsgs == nil {
			wantMsgs = inputs
		}
		for _, msg := range inputs {
			_, err := io.WriteString(forwarder, msg)
			if err != nil {
				t.Fatal(err)
			}
		}

		for _, want := range wantMsgs {
			select {
			case err := <-errs:
				t.Errorf("unexpected error: %v", err)
			case got := <-msgs:
				if !strings.HasSuffix(got, want) {
					t.Errorf("wrong result: want suffix %q, got %q", want, got)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("timed out")
			}
		}
	}
}

func TestSyslogTransport(t *testing.T) {
	t.Run("tcp", testTransport(servConfig{protocol: "tcp"}, nil, nil))
	t.Run("udp", testTransport(servConfig{protocol: "udp"}, nil, nil))
	t.Run("multiple-writes", testTransport(servConfig{protocol: "tcp"}, []string{"hello", "world"}, nil))

	tmp := maxLogBytes
	maxLogBytes = 10
	defer func() { maxLogBytes = tmp }()
	msgs := []string{"hello ", "world "}
	want := msgs[1:]
	t.Run("buffer-wrap", testTransport(servConfig{protocol: "tcp"}, msgs, want))
}

func TestRoundTrip(t *testing.T) {
	// TODO: test full integration from layer configuration to syslog destination server message receipt
}

func TestSyslogTransport_reconnect(t *testing.T) {
	config := servConfig{protocol: "tcp"}
	errs := make(chan error, 10)
	msgs := make(chan string, 20)
	crash := newCrash(postLengthRead)
	addr, closer, wg := startTestServer(t, config, crash, msgs, errs)

	defer wg.Wait()
	defer closer.Close()

	backend, err := NewSyslogBackend(addr)
	if err != nil {
		t.Fatal(err)
	}
	dest := NewLogDestination("testdest", backend)
	defer dest.Close()
	forwarder := NewLogForwarder(dest, "testservice")

	// write initial data, send to server
	_, err = io.WriteString(forwarder, "test1")
	if err != nil {
		t.Fatal(err)
	}

	// crash the server
	crash.trigger <- true

	// make sure it crashed
	select {
	case err := <-errs:
		if err.Error() != "server crashed" {
			t.Errorf("wanted crash error, got: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out")
	}

	// write some data while server is down
	_, err = io.WriteString(forwarder, "test2")
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.WriteString(forwarder, "test3")
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.WriteString(forwarder, "test4")
	if err != nil {
		t.Fatal(err)
	}

	// empty out the errors
	for len(errs) > 0 {
		<-errs
	}

	// start a new server and redirect the dest to it
	addr2, closer2, wg2 := startTestServer(t, config, crash, msgs, errs)
	backend, err = NewSyslogBackend(addr2)
	if err != nil {
		t.Fatal(err)
	}
	dest.SetBackend(backend)

	defer wg2.Wait()
	defer closer2.Close()

	select {
	case got := <-msgs:
		want := "test2"
		if !strings.HasSuffix(got, want) {
			t.Errorf("post server reboot - wrong message: want suffix %q, got %q", want, got)
		}
	case err := <-errs:
		t.Errorf("got unexpected error: %v", err)
	case <-time.After(4 * time.Second):
		t.Fatal("timed out")
	}
}

type servConfig struct {
	protocol string
	privkey  []byte
}

type whereCrash int

const (
	none whereCrash = iota
	postLengthRead
	postBodyRead
)

type crashConfig struct {
	where   whereCrash
	trigger chan bool
}

var noCrash = crashConfig{none, make(chan bool, 1)}

func newCrash(where whereCrash) crashConfig {
	return crashConfig{where: where, trigger: make(chan bool, 1)}
}

func startTestServer(t *testing.T, config servConfig, crash crashConfig, msgs chan<- string, errs chan<- error) (addr string, closer io.Closer, wg *sync.WaitGroup) {
	wg = new(sync.WaitGroup)

	var err error
	var c io.Closer
	if config.protocol == "tcp" {
		var l net.Listener
		l, err = net.Listen(config.protocol, "127.0.0.1:0")
		if err != nil {
			t.Fatalf("startServer failed: %v", err)
		}
		c = l
		wg.Add(1)
		go func() {
			defer wg.Done()
			runListenerServer(t, l, crash, msgs, errs, wg)
		}()
		addr = l.Addr().String()
	} else if config.protocol == "udp" {
		udpaddr := &net.UDPAddr{Port: 12255, IP: net.ParseIP("127.0.0.1")}
		conn, err := net.ListenUDP(config.protocol, udpaddr)
		if err != nil {
			t.Fatalf("startServer failed: %v", err)
		}
		c = conn
		wg.Add(1)
		go func() {
			defer wg.Done()
			processMessage(conn, crash, msgs, errs)
		}()
		addr = udpaddr.String()
	} else {
		t.Fatalf("unsupported test server protocol %q", config.protocol)
	}

	return addr, c, wg
}

func runListenerServer(t *testing.T, l net.Listener, crash crashConfig, msgs chan<- string, errs chan<- error, wg *sync.WaitGroup) {
	for {
		var c net.Conn
		var err error
		err = nil
		if c, err = l.Accept(); err != nil {
			if !errors.Is(err, net.ErrClosed) {
				t.Logf("server failed to accept connection: %v", err)
			}
			return
		}
		var crashed bool
		wg.Add(1)
		go func() {
			defer wg.Done()
			crashed = processMessage(c, crash, msgs, errs)
		}()
		if crashed {
			break
		}
	}
}

func tryCrash(where whereCrash, c crashConfig) error {
	if c.where != where {
		return nil
	}
	select {
	case <-c.trigger:
		return fmt.Errorf("server crashed")
	default:
	}
	return nil
}

func processMessage(c net.Conn, crash crashConfig, msgs chan<- string, errs chan<- error) (crashed bool) {
	defer c.Close()
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	b := bufio.NewReader(c)
	for n := 0; n < 10; n++ {
		s, err := b.ReadString(' ')
		if err != nil && errs != nil {
			errs <- err
		}

		if err := tryCrash(postLengthRead, crash); err != nil {
			errs <- err
			return true
		}

		i, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil {
			if errs != nil {
				errs <- err
			}
			break
		}

		msg := make([]byte, i)
		_, err = io.ReadFull(b, msg)
		if err != nil && errs != nil {
			errs <- err
		}

		if err := tryCrash(postBodyRead, crash); err != nil {
			errs <- err
			return true
		}

		msgs <- string(msg)
	}
	return false
}
