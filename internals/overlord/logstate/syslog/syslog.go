package syslog

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/canonical/pebble/internals/servicelog"
)

const (
	maxRequestEntries          = 100
	dialTimeout                = 10 * time.Second
	canonicalPrivEnterpriseNum = 28978
)

// Syslog Priority values - see RFC 5424 6.2.1
const (
	facilityUserLevelMessage = 1
	severityNotice           = 5
)

type ClientOptions struct {
	MaxRequestEntries int
	TargetName        string
	Location          string
	Hostname          string
	SDID              string
	DialTimeout       time.Duration
}

type entryWithService struct {
	service   string
	Timestamp string
	Message   string
}

type Client struct {
	options *ClientOptions

	// To store log entries, keep a buffer of size 2*MaxRequestEntries with a
	// sliding window "entries" of size MaxRequestEntries.
	buffer  []entryWithService
	entries []entryWithService

	// Store the custom labels(syslog's structured-data) for each service
	labels map[string]string

	// connection info
	conn     net.Conn
	location *url.URL
	closed   bool
	sendBuf  bytes.Buffer
}

// priorityVal calculates the syslog Priority value (PRIVAL) from the given
// Facility and Severity values. See RFC 5424, sec 6.2.1 for details.
func priorityVal(facility, severity int) int {
	return facility*8 + severity
}

func fillDefaultOptions(options *ClientOptions) {
	if options.MaxRequestEntries == 0 {
		options.MaxRequestEntries = maxRequestEntries
	}
	if options.DialTimeout == 0 {
		options.DialTimeout = dialTimeout
	}
}

// NewClient creates a syslog client.
func NewClient(options *ClientOptions) (*Client, error) {

	opts := *options
	fillDefaultOptions(&opts)

	if len(opts.Hostname) == 0 {
		hostname, err := os.Hostname()
		if err != nil {
			hostname = "-"
		}
		opts.Hostname = hostname
	}

	u, err := url.Parse(opts.Location)
	if err != nil {
		return nil, fmt.Errorf("invalid syslog server location: %v", err)
	}
	if u.Scheme != "tcp" || u.Host == "" { // may support udp later
		return nil, fmt.Errorf("invalid syslog server location %s, syslog server location must be in form 'tcp://host:port'", opts.Location)
	}

	c := &Client{
		location: u,
		options:  &opts,
		buffer:   make([]entryWithService, 2*opts.MaxRequestEntries),
		labels:   make(map[string]string),
	}
	c.entries = c.buffer[:0]
	return c, nil
}

// SetLabels formats the given service's labels into a structured-data section
// for a syslog message, according to RFC5424 section 6
func (c *Client) SetLabels(serviceName string, labels map[string]string) {
	if len(labels) == 0 {
		delete(c.labels, serviceName)
		return
	}
	var buf bytes.Buffer

	if len(c.options.SDID) > 0 {
		fmt.Fprintf(&buf, "[%s@%d", c.options.SDID, canonicalPrivEnterpriseNum)
	} else {
		fmt.Fprintf(&buf, "[%s@%d", "pebble", canonicalPrivEnterpriseNum)
	}

	// Sort label keys to get deterministic output
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		fmt.Fprintf(&buf, " %s=\"", key)
		value := labels[key]
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
	c.labels[serviceName] = buf.String()
}

func (c *Client) Add(entry servicelog.Entry) error {
	if len(c.entries) >= c.options.MaxRequestEntries {
		// 'entries' is full - remove the first element to make room
		// Zero the removed element to allow garbage collection
		c.entries[0] = entryWithService{}
		c.entries = c.entries[1:]
	}

	if len(c.entries) >= cap(c.entries) {
		// Copy all the elements to the start of the buffer
		copy(c.buffer, c.entries)

		// Reset the view into the buffer
		c.entries = c.buffer[:len(c.entries):len(c.buffer)]

		// Zero removed elements to allow garbage collection
		for i := len(c.entries); i < len(c.buffer); i++ {
			c.buffer[i] = entryWithService{}
		}
	}

	entry.Message = strings.TrimSuffix(entry.Message, "\n")

	c.entries = append(c.entries, entryWithService{
		Timestamp: entry.Time.Format(time.RFC3339Nano), // Format: 2021-05-26T12:37:01Z
		Message:   entry.Message,
		service:   entry.Service,
	})
	return nil
}

func (c *Client) ensureConnected(ctx context.Context) error {
	if c.conn != nil {
		return nil
	} else if c.closed {
		return fmt.Errorf("write to closed SyslogBackend")
	}

	d := net.Dialer{Timeout: c.options.DialTimeout}
	conn, err := d.DialContext(ctx, c.location.Scheme, c.location.Host)
	if err != nil {
		return fmt.Errorf("cannot connect to %s", c.location)
	}

	c.conn = conn
	return nil
}

func (c *Client) Close() error {
	c.closed = true
	if c.conn == nil {
		return nil
	}

	err := c.conn.Close()
	c.conn = nil
	return err
}

// Flush sends buffered logs to the syslog endpoint.
func (c *Client) Flush(ctx context.Context) error {
	if len(c.entries) == 0 {
		return nil
	}

	err := c.ensureConnected(ctx)
	if err != nil {
		return err
	}
	c.sendBuf.Reset()

	for _, entry := range c.entries {
		structuredData, ok := c.labels[entry.service]
		if !ok {
			structuredData = "-"
		}

		defaultPriority := priorityVal(facilityUserLevelMessage, severityNotice)

		// Build the message first to calculate length
		var msgBuf bytes.Buffer
		msgBuf.WriteByte('<')
		// Convert priority to string without fmt.Sprint
		priorityStr := strconv.Itoa(defaultPriority)
		msgBuf.WriteString(priorityStr)
		msgBuf.WriteString(">1 ")
		msgBuf.WriteString(entry.Timestamp)
		msgBuf.WriteByte(' ')
		msgBuf.WriteString(c.options.Hostname)
		msgBuf.WriteByte(' ')
		msgBuf.WriteString(entry.service)
		msgBuf.WriteString(" - - ")
		msgBuf.WriteString(structuredData)
		msgBuf.WriteByte(' ')
		msgBuf.WriteString(entry.Message)

		// Octet framing as per RFC 5425: <length> <message>
		lengthStr := strconv.Itoa(msgBuf.Len())
		c.sendBuf.WriteString(lengthStr)
		c.sendBuf.WriteByte(' ')
		c.sendBuf.Write(msgBuf.Bytes())
	}

	_, err = io.Copy(c.conn, &c.sendBuf)
	if err != nil {
		// The connection might be bad. Close and reset it for later reconnection attempt(s).
		c.conn.Close()
		c.conn = nil
		return fmt.Errorf("cannot send syslog: %v", err)
	}

	c.resetBuffer()
	return nil
}

func (c *Client) resetBuffer() {
	for i := 0; i < len(c.entries); i++ {
		c.entries[i] = entryWithService{}
	}
	c.entries = c.buffer[:0]
}
