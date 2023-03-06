// Copyright (c) 2023 Canonical Ltd
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

package logstate

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"time"

	"github.com/canonical/pebble/internal/servicelog"
)

const (
	canonicalPrivEnterpriseNum = 28978

	syslogInitialBackoff = 100 * time.Millisecond
	syslogMaxBackoff     = 10 * time.Second
)

type syslogClient struct {
	// metadata
	version        int
	host           string
	msgid          string
	priority       int
	pid            string
	structuredData string // TODO: use this for future log labels

	// connection info
	conn          net.Conn
	address       *url.URL
	waitReconnect time.Duration
	closed        bool

	data bytes.Buffer
}

func newSyslogClient(addr string) (*syslogClient, error) {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "-"
	}

	u, err := url.Parse(addr)
	if err != nil || u.Host == "" {
		u, err = url.Parse("//" + addr)
	}
	if err != nil {
		return nil, err
	} else if u.Scheme != "tcp" && u.Scheme != "udp" && u.Scheme != "" {
		return nil, fmt.Errorf("invalid syslog server address scheme %q", u.Scheme)
	}
	if u.Scheme == "" {
		u.Scheme = "tcp"
	}

	return &syslogClient{
		version:        1, // RFC5424
		pid:            "-",
		host:           hostname,
		msgid:          "-",
		priority:       priorityVal(FacilityUserLevelMessage, SeverityInformational),
		structuredData: syslogStructuredData("pebble", canonicalPrivEnterpriseNum, nil),
		address:        u,
	}, nil
}

// Syslog Priority values - see RFC 5424 6.2.1
const (
	// Facility values
	FacilityUserLevelMessage = 1

	// Severity values
	SeverityInformational = 6
)

// priorityVal calculates the syslog Priority value (PRIVAL) from the given
// Facility and Severity values. See RFC 5424, sec 6.2.1 for details.
func priorityVal(facility, severity int) int {
	return facility*8 + severity
}

// syslogStructuredData formats the given labels into a structured data section
// for a syslog message, according to RFC5424 section 6.
func syslogStructuredData(name string, enterpriseNum int, labels map[string]string) string {
	if len(labels) == 0 {
		return "-"
	}
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "[%s@%d", name, enterpriseNum)
	for key, value := range labels {
		fmt.Fprintf(&buf, " %s=\"", key)
		// escape the value according to RFC5424 6.3.3
		for i := 0; i < len(value); i++ {
			// don't use "for _, c := range value" as we don't want runes
			c := value[i]
			if c == '"' || c == '\\' || c == ']' {
				buf.WriteByte('\\')
			}
			buf.WriteByte(c)
		}
		buf.WriteByte('"')
	}
	buf.WriteByte(']')
	return buf.String()
}

func (c *syslogClient) encodeEntry(entry servicelog.Entry) {
	// format defined by RFC 5424
	timestamp := entry.Time.Format(time.RFC3339)
	msg := fmt.Sprintf("<%d>%d %s %s %s %s %s %s %s",
		c.priority, c.version, timestamp, c.host, entry.Service,
		c.pid, c.msgid, c.structuredData, entry.Message)

	// Octet framing as per RFC 5425.
	framed := fmt.Sprintf("%d %s", len(msg), msg)
	c.data.Write([]byte(framed))
}

// Send messages to remote syslog server.
func (c *syslogClient) Send(entries []servicelog.Entry) error {
	err := c.ensureConnected()
	if err != nil {
		return err
	}

	c.data.Reset()
	for _, entry := range entries {
		c.encodeEntry(entry)
	}

	_, err = io.Copy(c.conn, &c.data)
	if err != nil {
		// The connection might be bad. Close and reset it for later reconnection attempt(s).
		c.conn.Close()
		c.conn = nil
	}
	return err
}

func (c *syslogClient) ensureConnected() error {
	if c.conn != nil {
		return nil
	} else if c.closed {
		return fmt.Errorf("write to closed SyslogBackend")
	}

	if c.waitReconnect > 0 {
		time.Sleep(c.waitReconnect)
	}

	conn, err := net.Dial(c.address.Scheme, c.address.Host)
	if err != nil {
		// start an exponential backoff for reconnection attempts
		if c.waitReconnect == 0 {
			c.waitReconnect = syslogInitialBackoff
		}
		newWait := 2 * c.waitReconnect
		if newWait > syslogMaxBackoff {
			newWait = syslogMaxBackoff
		}
		c.waitReconnect = newWait
		return err
	}

	c.waitReconnect = 0 // reset backoff
	c.conn = conn
	return nil
}

func (c *syslogClient) Close() error {
	c.closed = true
	if c.conn == nil {
		return nil
	}

	err := c.conn.Close()
	c.conn = nil
	return err
}
