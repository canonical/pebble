package logstate

import (
	"context"
	"fmt"
	"time"

	"github.com/canonical/pebble/internals/servicelog"
)

// Fake sample implementations of logClient
// TODO: remove this file before merging

type nonBufferingClient struct{}

var _ logClient = &nonBufferingClient{}

func (c *nonBufferingClient) Write(_ context.Context, entry servicelog.Entry) error {
	fmt.Printf("%v [%s] %s", entry.Time, entry.Service, entry.Message)
	return nil
}

func (c *nonBufferingClient) Flush(_ context.Context) error {
	// no-op
	return nil
}

type bufferingClient struct {
	entries   []servicelog.Entry
	threshold int
}

var _ logClient = &bufferingClient{}

func (c *bufferingClient) Write(ctx context.Context, entry servicelog.Entry) error {
	c.entries = append(c.entries, entry)
	if c.threshold > 0 && len(c.entries) >= c.threshold {
		return c.Flush(ctx)
	}
	return nil
}

func (c *bufferingClient) Flush(_ context.Context) error {
	for _, entry := range c.entries {
		fmt.Printf("%v [%s] %s", entry.Time, entry.Service, entry.Message)
	}
	fmt.Println()
	c.entries = c.entries[:0]
	return nil
}

// a slow client where Flush takes a long time
type slowClient struct {
	flushTime time.Duration
}

var _ logClient = &slowClient{}

func (c *slowClient) Write(_ context.Context, _ servicelog.Entry) error {
	return nil
}

func (c *slowClient) Flush(ctx context.Context) error {
	select {
	case <-time.After(c.flushTime):
		return nil
	case <-ctx.Done():
		return fmt.Errorf("timeout flushing logs")
	}
}
