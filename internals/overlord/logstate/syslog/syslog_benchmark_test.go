package syslog

import (
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
		client.buildSendBuffer()
	}
}
