package servicelog_test

import (
	"bufio"
	"crypto/tls"
	"io"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/canonical/pebble/internal/servicelog"
)

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

func startTestServer(msgs chan<- string, errs chan<- error) (addr string, closer io.Closer, wg *sync.WaitGroup) {
	wg = new(sync.WaitGroup)

	cert, err := tls.X509KeyPair(testCert, testPrivKey)
	if err != nil {
		log.Fatalf("failed to load TLS keypair: %v", err)
	}
	config := tls.Config{Certificates: []tls.Certificate{cert}}
	l, e := tls.Listen("tcp", "127.0.0.1:0", &config)
	if e != nil {
		log.Fatalf("startServer failed: %v", e)
	}
	addr = l.Addr().String()
	wg.Add(1)
	go func() {
		defer wg.Done()
		runStreamSyslog(l, msgs, errs, wg)
	}()
	return addr, l, wg
}

func runStreamSyslog(l net.Listener, msgs chan<- string, errs chan<- error, wg *sync.WaitGroup) {
	for {
		var c net.Conn
		var err error
		if c, err = l.Accept(); err != nil {
			return
		}
		wg.Add(1)
		go func(c net.Conn) {
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
		}(c)
	}
}

func TestSyslogTransport(t *testing.T) {
	errs := make(chan error, 10)
	msgs := make(chan string)
	addr, closer, wg := startTestServer(msgs, errs)
	defer wg.Wait()
	defer closer.Close()

	transport := servicelog.NewSyslogTransport("tcp", addr, testCert)
	w := servicelog.NewSyslogWriter(transport, "testapp")

	want := "hello"
	_, err := io.WriteString(w, want)
	if err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-errs:
		t.Error(err)
	case got := <-msgs:
		if !strings.HasSuffix(got, want) {
			t.Errorf("wrong result: want suffix '%v', got '%v'", want, got)
		}
	}
}
