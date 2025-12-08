package syslog

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/canonical/pebble/internals/servicelog"
)

func BenchmarkEncodeEntry(b *testing.B) {
	client, err := NewClient(&ClientOptions{
		Location: "tcp://dummy:1234",
		Hostname: "myhostname",
		SDID:     "pebble",
	})
	if err != nil {
		b.Fatalf("Failed to create syslog client: %v", err)
	}

	entry := servicelog.Entry{
		Time:    time.Date(2025, 1, 2, 15, 4, 5, 123456789, time.UTC),
		Service: "redis",
		Message: "This is a reasonably long log message to benchmark the encodeEntry function.",
	}
	client.SetLabels("svc1", map[string]string{
		"env":     "test",
		"version": "0.0.1",
	})
	client.Add(entry)

	b.ResetTimer()
	for b.Loop() {
		client.buildSendBufferTCP()
	}
}

func BenchmarkFlushUDP(b *testing.B) {
	client, err := NewClient(&ClientOptions{
		Location: "udp://dummy:1234",
		Hostname: "test-machine",
		SDID:     "test-sdid",
	})
	if err != nil {
		b.Fatalf("Failed to create syslog client: %v", err)
	}
	defer client.Close()

	client.conn = &doNothingConn{}

	entry := servicelog.Entry{
		Time:    time.Date(2025, 1, 2, 15, 4, 5, 123456789, time.UTC),
		Service: "redis",
		Message: "This is a reasonably long log message to benchmark the flushUDP function.",
	}
	client.SetLabels("redis", map[string]string{
		"env":     "test",
		"version": "0.0.1",
	})

	b.ResetTimer()
	for b.Loop() {
		client.Add(entry)
		err = client.Flush(context.Background())
		if err != nil {
			b.Fatalf("Failed to flush entries: %v", err)
		}
	}
}

type doNothingConn struct{}

func (d *doNothingConn) Read(b []byte) (n int, err error)   { return 0, nil }
func (d *doNothingConn) Write(b []byte) (n int, err error)  { return len(b), nil }
func (d *doNothingConn) Close() error                       { return nil }
func (d *doNothingConn) LocalAddr() net.Addr                { return nil }
func (d *doNothingConn) RemoteAddr() net.Addr               { return nil }
func (d *doNothingConn) SetDeadline(t time.Time) error      { return nil }
func (d *doNothingConn) SetReadDeadline(t time.Time) error  { return nil }
func (d *doNothingConn) SetWriteDeadline(t time.Time) error { return nil }
