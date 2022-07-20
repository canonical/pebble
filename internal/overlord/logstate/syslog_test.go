package logstate

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type servConfig struct {
	protocol string
	cert     []byte
	privkey  []byte
}

func startTestServer(t *testing.T, config servConfig, msgs chan<- string, errs chan<- error) (addr string, closer io.Closer, wg *sync.WaitGroup) {
	wg = new(sync.WaitGroup)

	var err error
	var c io.Closer
	if config.protocol == "tcp" {
		var l net.Listener
		l, err = net.Listen(config.protocol, "127.0.0.1:0")
		if err != nil {
			t.Fatalf("startServer failed: %v", err)
		}
		if config.cert != nil {
			cert, err := tls.X509KeyPair(config.cert, config.privkey)
			if err != nil {
				t.Fatalf("failed to load TLS keypair: %v", err)
			}
			tlsconfig := &tls.Config{Certificates: []tls.Certificate{cert}}
			l = tls.NewListener(l, tlsconfig)
		}
		c = l
		wg.Add(1)
		go func() {
			defer wg.Done()
			runListenerServer(t, l, msgs, errs, wg)
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
			runConnServer(t, conn, msgs, errs, wg)
		}()
		addr = udpaddr.String()
	}

	return addr, c, wg
}

func runListenerServer(t *testing.T, l net.Listener, msgs chan<- string, errs chan<- error, wg *sync.WaitGroup) {
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
		wg.Add(1)
		go processMessage(c, msgs, errs, wg)
	}
}
func runConnServer(t *testing.T, c net.Conn, msgs chan<- string, errs chan<- error, wg *sync.WaitGroup) {
	wg.Add(1)
	processMessage(c, msgs, errs, wg)
}

func processMessage(c net.Conn, msgs chan<- string, errs chan<- error, wg *sync.WaitGroup) {
	defer wg.Done()
	defer c.Close()
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	b := bufio.NewReader(c)
	for n := 0; n < 10; n++ {
		s, err := b.ReadString(' ')
		if err != nil {
			if errs != nil {
				errs <- err
			}
			break
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
		if err != nil {
			if errs != nil {
				errs <- err
			}
			break
		}
		msgs <- string(msg)
	}
}

func TestMain(m *testing.M) {
	timeFunc = func() time.Time {
		return time.Time{}
	}
	m.Run()
}

func TestSyslogWriter(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		pid    int
		params map[string]string
		want   string
	}{
		{
			name:  "basic",
			input: "hello",
			want:  "<14>1 0001-01-01T00:00:00Z - testapp 0 - - hello",
		}, {
			name:   "basic-param",
			input:  "hello",
			params: map[string]string{"foo": "bar"},
			want:   "<14>1 0001-01-01T00:00:00Z - testapp 0 - [pebble@28978 foo=\"bar\"] hello",
		}, {
			name:   "basic-multiparam",
			input:  "hello",
			params: map[string]string{"foo": "bar", "baz": "quux"},
			want:   "<14>1 0001-01-01T00:00:00Z - testapp 0 - [pebble@28978 foo=\"bar\" baz=\"quux\"] hello",
		}, {
			name:  "basic-pid",
			input: "hello",
			pid:   42,
			want:  "<14>1 0001-01-01T00:00:00Z - testapp 42 - - hello",
		}, {
			name:   "param-escapes",
			input:  "hello",
			params: map[string]string{"foo": "\"[bar]\\"},
			want:   "<14>1 0001-01-01T00:00:00Z - testapp 0 - [pebble@28978 foo=\"\\\"[bar\\]\\\\\"] hello",
		},
	}

	for i, test := range tests {
		t.Run(fmt.Sprintf("case %v (%v)", i+1, test.name), func(t *testing.T) {
			var buf bytes.Buffer
			w := NewSyslogWriter(&buf, "testapp", test.params)
			w.SetPid(test.pid)

			_, err := io.WriteString(w, test.input)
			if err != nil {
				t.Errorf("write failed: %v", err)
				return
			}

			if got := buf.String(); got != test.want {
				t.Errorf("wrong output:\nwant %q\ngot  %q", test.want, got)
			}
		})
	}
}

func testTransport(config servConfig) func(t *testing.T) {
	return func(t *testing.T) {
		errs := make(chan error, 10)
		msgs := make(chan string)
		addr, closer, wg := startTestServer(t, config, msgs, errs)

		defer wg.Wait()
		defer closer.Close()

		transport := NewSyslogTransport(config.protocol, addr, config.cert)

		want := "hello"
		_, err := io.WriteString(transport, want)
		if err != nil {
			t.Fatal(err)
		}

		select {
		case err := <-errs:
			t.Error("unexpected error:", err)
		case got := <-msgs:
			if got != want {
				t.Errorf("wrong result: want %q, got %q", want, got)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("timed out")
		}
	}
}

func TestSyslogTransport(t *testing.T) {
	t.Run("tcp", testTransport(servConfig{protocol: "tcp"}))
	t.Run("udp", testTransport(servConfig{protocol: "udp"}))
	t.Run("TLS-tcp", testTransport(servConfig{protocol: "tcp", cert: testCert, privkey: testPrivKey}))
}

func TestRoundTrip(t *testing.T) {
	// TODO: test full integration from layer configuration to syslog destination server message receipt
}

func TestSyslogTransport_wraparound(t *testing.T) {
	// TODO: test that message writes don't get munged when the ringbuffer gets full and wraps.
}

func TestSyslogTransport_backoff(t *testing.T) {
	// TODO: test the exponential backoff on reconnect attempts works
}

func TestSyslogTransport_reconnect(t *testing.T) {
	// TODO: check that we can successfully reconnect without dropping continued incoming logs.
}

var testCert = []byte(`
-----BEGIN CERTIFICATE-----
MIIC4jCCAcqgAwIBAgIQUfp0amlHQ2i3/siLio24lTANBgkqhkiG9w0BAQsFADAU
MRIwEAYDVQQDDAkxMjcuMC4wLjEwIBcNMjIwNjI2MDk1NjQ2WhgPMjExMjEyMzEw
MDAwMDBaMBQxEjAQBgNVBAMMCTEyNy4wLjAuMTCCASIwDQYJKoZIhvcNAQEBBQAD
ggEPADCCAQoCggEBANsjo9YNaPRMaAVqJZ1/8KoW9KwyscSZNXsegCzomkK4lztE
6XWDKqNLat6uMX4eo4uQEyLtSYsEUR7lTMOVcWa9i1rG+R0S++rJr7yOqqV/REVe
hMK+UCYKM80OGf/BUkF3iquTa1xA9AFfBnxgEi1APE1SPsXDb4dmwNHS7rwvaZn0
k2xO1PCJgb5+CAptZMIqaS4uKVaDQ9G3ExLmEbZD8hWk5XDE9Kxi/NGO8Iid1RmL
LAMPn5VfnYyl6fapvTg54jISTZ4ELpTW5S+nsTUfqU8GEihMhPfLBZSR8jgdEmmL
bzMewKf4Z7lEqhqmfvk64PhZWCQcxcJjFOnO9NcCAwEAAaMuMCwwDwYDVR0RBAgw
BocEfwAAATALBgNVHQ8EBAMCAa4wDAYDVR0TBAUwAwEB/zANBgkqhkiG9w0BAQsF
AAOCAQEAVI03eM40/58btyB4rG4yOrvIYZKPc2l7Q1r7fjMneJDzsMQq6ctLFGhB
HEZFnN8BGZxrTmZBRkehJTuW7GQQA5ThCclS207ofWP59iwruqBKZxKHmRQV2Nsx
mpLTBF4jFM9RS+92Zu+jXgKPeCtEvJEf6TOZfnaUCnvwooIcmUfrC7lHe3GOqyCm
u6l2Zl7/6jFf04uVhQpoeud4iFfNhzasULPtuxotH598VSwgc8Qk0WaIQfsYxVZr
0mRV0n/IaflehPm4l4Se8OJ9l1vq/4xFZ1kqPKiXMr79YTfW8lzps0ztVEFIE7wr
7DFOUCcuTqZbuBJp2DH5t6k0F7q0MA==
-----END CERTIFICATE-----
`)

var testPrivKey = []byte(`
-----BEGIN PRIVATE KEY-----
MIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEAAoIBAQDbI6PWDWj0TGgF
aiWdf/CqFvSsMrHEmTV7HoAs6JpCuJc7ROl1gyqjS2rerjF+HqOLkBMi7UmLBFEe
5UzDlXFmvYtaxvkdEvvqya+8jqqlf0RFXoTCvlAmCjPNDhn/wVJBd4qrk2tcQPQB
XwZ8YBItQDxNUj7Fw2+HZsDR0u68L2mZ9JNsTtTwiYG+fggKbWTCKmkuLilWg0PR
txMS5hG2Q/IVpOVwxPSsYvzRjvCIndUZiywDD5+VX52Mpen2qb04OeIyEk2eBC6U
1uUvp7E1H6lPBhIoTIT3ywWUkfI4HRJpi28zHsCn+Ge5RKoapn75OuD4WVgkHMXC
YxTpzvTXAgMBAAECggEABnRr5Jr+6gZy7SK0FCORaMrF6JySdLWelNw2u+i0dbd1
abi2t7aZXVEtxxYsCjvIAKbJwxzO+1oXmZvF2xNeN7+1iLYu3rBeEAuVmMdxaom8
VRH0dYs6VfFO/U2nbmUfXlZLFPeB3g6VVJHPLkxJVmCuzG6m8rEcnXxlVmJ3YPIA
OHL29+txeCXaVqphXR0Pi/mMaetnxOCDaFUv03BoHu6e6OL9jyh2lfGaRgFwFUa7
OmmvdxIGPyCrNWMUxix3zvU1PtKF5AI8uEe7cGeksSKVRVKwo63yClILSRrWL/7x
n2raRpAjYaBogn2sLyjSCGxdnszGSJkoFMguTQQhAQKBgQDqWXMixIekJdjRUipt
nHrfE/nwSol8zpDq/f9ykErudmugv0+tQGT6mjydMRb5dy6A/r8eEOaVEuaiHAXB
LU/xNfuP1I0gzR+GdMDmvAqnImcmazM3sUbjCsJKUSqDNzn77kzY1CQoz3oDitwq
lDhtor8e4mYwx1N8b9dYeKGLYQKBgQDvYnT/mmiwyv4EzbrTriE4oFu0y7DBtkV8
QXGV0WbJXv2hSjLW75pO8Y3/IpNgzNgvxouDgHHAaSeDEjfnDiVWN3kR23ttFxvw
rTwDGfs1rgeMjofnhK7wgTvCzXdw9jviZThr9WpeUmvcNhNgOUYgBpiplULKj+QJ
n1JbGmPjNwKBgQCb2WD4fjq2r3TBwCL3Qll0gZR2eRt2JOm7Xa/EQLGUZKyu+ovC
bFC7WFd3Mm5U+S20G7Z+CD9QZIF8zaYGElxXzc6+mFxCtCeDA6JF0EhFXlu68Q/e
ucaqtzz+r3vWR6QIJzJ0AKELgu9h67b/mhLs1o7Du0y6o9ShrL9J1u+YAQKBgFaZ
2tPBa5BRz3Wza5w6yX/v211btxVNOHQMROg7OiEtgToBWsURJ1TZ5FHhk0mYsbkO
7dfj9sLyB75OL/Uh0/YN2XnRWiSMEKqQMT65/nxb+hUqVxY1lQgi6Ji/ti8ilWWA
0tmTjiiTTrv6wCW2cp0RZdcrzV70kT296pBUysAfAoGABle8K4/DZrpawvAY0hep
x//jQVuTKjlxaxl5StqK64eY+3aPVw5Onl7xTQVwujULwVECKTuX4I4EuiursxZ7
UNObMyzYtoBJl1gc9PObkuYS5ZfNhkIZDvsV+uva0pLcqhoTHtGw4m76guUDACOV
d6XdnEpQhZ8XMgzxgV4n7FA=
-----END PRIVATE KEY-----
`)
