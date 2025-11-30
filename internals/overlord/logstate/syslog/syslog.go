package syslog

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/url"
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
	initialSendBufferSize      = 4 * 1024
)

type ClientOptions struct {
	MaxRequestEntries int
	Location          string
	Hostname          string
	SDID              string
	DialTimeout       time.Duration
}

type entryWithService struct {
	service   string
	timestamp string
	message   string
}

type Client struct {
	options *ClientOptions

	// To store log entries, keep a buffer of size 2*MaxRequestEntries with a
	// sliding window "entries" of size MaxRequestEntries.
	buffer  []entryWithService
	entries []entryWithService

	// Store the custom labels (syslog's structured-data) for each service
	labels map[string]string

	// connection info
	conn     net.Conn
	location *url.URL
	closed   bool

	sendBuf bytes.Buffer
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

	u, err := url.Parse(opts.Location)
	if err != nil {
		return nil, fmt.Errorf("invalid syslog server location: %v", err)
	}
	if (u.Scheme != "tcp" && u.Scheme != "udp") || u.Host == "" {
		return nil, fmt.Errorf(`invalid syslog server location %q, must be in form "tcp://host:port" or "udp://host:port"`, opts.Location)
	}

	c := &Client{
		location: u,
		options:  &opts,
		buffer:   make([]entryWithService, 2*opts.MaxRequestEntries),
		labels:   make(map[string]string),
	}
	c.entries = c.buffer[:0]
	c.sendBuf.Grow(initialSendBufferSize)

	return c, nil
}

// SetLabels formats the given service's labels into a structured-data section
// for a syslog message, according to RFC5424 section 6
func (c *Client) SetLabels(serviceName string, labels map[string]string) {
	if labels == nil {
		delete(c.labels, serviceName)
		return
	}
	var buf strings.Builder

	sdID := c.options.SDID
	if sdID == "" {
		sdID = "pebble"
	}
	fmt.Fprintf(&buf, "[%s@%d", sdID, canonicalPrivEnterpriseNum)

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
		timestamp: entry.Time.Format(time.RFC3339Nano), // Format: 2021-05-26T12:37:01.123456789Z
		message:   entry.Message,
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
		return fmt.Errorf("cannot connect to %s: %w", c.location, err)
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

func (c *Client) buildSendBuffer() {
	c.sendBuf.Reset()
	hostname := c.options.Hostname
	if hostname == "" {
		hostname = "-"
	}

	isTCP := c.location.Scheme == "tcp"

	for _, entry := range c.entries {
		structuredData, ok := c.labels[entry.service]
		if !ok {
			structuredData = "-"
		}

		// Message format as per RFC 5424
		const prefix = "<13>1 " // priority 13 = 1*8+5 (facility user, priority notice), version 1

		if isTCP {
			// TCP: Octet framing as per RFC 5425: <length> <message>
			frameLength := len(prefix) + len(entry.timestamp) + 1 + len(hostname) + 1 +
				len(entry.service) + 5 + len(structuredData) + 1 + len(entry.message)
			lengthBuf := make([]byte, 0, 8)
			lengthBuf = strconv.AppendInt(lengthBuf, int64(frameLength), 10)
			c.sendBuf.Write(lengthBuf)
			c.sendBuf.WriteByte(' ')
		}

		c.sendBuf.WriteString(prefix)
		c.sendBuf.WriteString(entry.timestamp)
		c.sendBuf.WriteByte(' ')
		c.sendBuf.WriteString(hostname)
		c.sendBuf.WriteByte(' ')
		c.sendBuf.WriteString(entry.service)
		c.sendBuf.WriteString(" - - ")
		c.sendBuf.WriteString(structuredData)
		c.sendBuf.WriteByte(' ')
		c.sendBuf.WriteString(entry.message)
	}
}

// Flush sends buffered logs to the syslog endpoint.
func (c *Client) Flush(ctx context.Context) error {
	if len(c.entries) == 0 {
		return nil
	}

	if c.location.Scheme == "udp" {
		return c.flushUDP(ctx)
	}
	return c.flushTCP(ctx)
}

func (c *Client) flushTCP(ctx context.Context) error {
	err := c.ensureConnected(ctx)
	if err != nil {
		return err
	}

	c.buildSendBuffer()
	_, err = io.Copy(c.conn, &c.sendBuf)
	if err != nil {
		// The connection might be bad. Close and reset it for later reconnection attempt(s).
		c.conn.Close()
		c.conn = nil
		return fmt.Errorf("cannot send syslogs: %w", err)
	}

	c.resetBuffer()
	return nil
}

func (c *Client) flushUDP(ctx context.Context) error {
	// For UDP, we send each message as a separate datagram (RFC 5426 section 3.1)
	// UDP connections don't need persistent state, so we create a fresh connection
	var d net.Dialer
	conn, err := d.DialContext(ctx, "udp", c.location.Host)
	if err != nil {
		return fmt.Errorf("cannot connect to %s: %w", c.location, err)
	}
	defer conn.Close()

	c.buildSendBuffer()
	_, err = io.Copy(conn, &c.sendBuf)
	if err != nil {
		return fmt.Errorf("cannot send syslogs: %w", err)
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
