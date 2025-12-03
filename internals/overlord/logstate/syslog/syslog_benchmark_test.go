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
	conn, err := net.ListenPacket("udp", "localhost:0")
	if err != nil {
		b.Fatalf("Failed to create UDP listener: %v", err)
	}
	defer conn.Close()

	go func() {
		for {
			buf := make([]byte, 8192)
			_, _, err := conn.ReadFrom(buf)
			if err != nil {
				break
			}
		}
	}()

	client, err := NewClient(&ClientOptions{
		Location: "udp://" + conn.LocalAddr().String(),
		Hostname: "test-machine",
		SDID:     "test-sdid",
	})
	if err != nil {
		b.Fatalf("Failed to create syslog client: %v", err)
	}
	defer client.Close()

	entries := []servicelog.Entry{
		{
			Time:    time.Date(2025, 1, 2, 15, 4, 5, 123456789, time.UTC),
			Service: "redis",
			Message: "This is a reasonably long log message to benchmark the flushUDP function.",
		}, {
			Time:    time.Date(2025, 1, 2, 15, 4, 6, 987654321, time.UTC),
			Service: "nginx",
			Message: "Another log message to include in the UDP flush benchmark.",
		},
	}
	client.SetLabels("redis", map[string]string{
		"env":     "test",
		"version": "0.0.1",
	})

	b.ResetTimer()
	for b.Loop() {
		for _, entry := range entries {
			client.Add(entry)
		}
		err = client.Flush(context.Background())
		if err != nil {
			b.Fatalf("Failed to flush entries: %v", err)
		}
	}
}
