package logstate

import (
	"io"
	"strings"
	"testing"
	"time"
)

func testMsg(msg string, n int) *LogMessage {
	return &LogMessage{Message: []byte(strings.Repeat(msg, n))}
}

func TestLogBuffer_Notify(t *testing.T) {
	capacity := 1000
	b := NewLogBuffer(capacity)

	ch := make(chan bool, 1)
	b.Notify(ch)

	err := b.Put(testMsg("foo", 1))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-ch:
	case <-time.After(100 * time.Millisecond):
		t.Error("failed to receive notification in time")
	}
}

// TestLogBuffer_NotifyDrop ensures that notification sends that block don't cause the buffer to
// sieze up.
func TestLogBuffer_NotifyDrop(t *testing.T) {
	capacity := 1000
	b := NewLogBuffer(capacity)

	ch := make(chan bool)
	b.Notify(ch)

	err := b.Put(testMsg("foo", 1))
	if err != nil {
		t.Fatal(err)
	}

	err = b.Put(testMsg("bar", 1))
	if err != nil {
		t.Fatal(err)
	}
}

func TestLogBuffer_OverCapacity(t *testing.T) {
	capacity := 5
	b := NewLogBuffer(capacity)
	err := b.Put(testMsg("a", capacity+1))
	if err == nil {
		t.Fatal("missing an expected over-capacity error")
	}
}

func TestLogBuffer_Overwrite(t *testing.T) {
	capacity := 5
	msglen := capacity/2 + 1
	b := NewLogBuffer(capacity)

	err := b.Put(testMsg("a", msglen))
	if err != nil {
		t.Fatal(err)
	}
	msg2 := testMsg("b", msglen)
	err = b.Put(msg2)
	if err != nil {
		t.Fatal(err)
	}
	msgs := b.GetAll()
	if len(msgs) != 1 {
		t.Errorf("wrong number of buffered messages: want 1, got %v", len(msgs))
	} else if got := string(msgs[0].Message); got != string(msg2.Message) {
		t.Errorf("didn't see expected FIFO buffer handling: wanted message %q, got %q", string(msg2.Message), got)
	}
}

func TestLogBuffer_GetAll(t *testing.T) {
	capacity := 1000
	b := NewLogBuffer(capacity)

	err := b.Put(testMsg("foo", 1))
	if err != nil {
		t.Fatal(err)
	}

	err = b.Put(testMsg("bar", 1))
	if err != nil {
		t.Fatal(err)
	}

	msgs := b.GetAll()
	want := 2
	if len(msgs) != want {
		t.Errorf("got wrong number messages: want %v, got %v", want, len(msgs))
	}

	msgs = b.GetAll()
	want = 0
	if len(msgs) != want {
		t.Errorf("got wrong number messages on second retrieval: want %v, got %v", want, len(msgs))
	}
}

type testBackend struct {
	msgs []*LogMessage
	ch   chan *LogMessage
}

func newTestBackend(ch chan *LogMessage) *testBackend {
	if ch == nil {
		ch = make(chan *LogMessage)
	}
	return &testBackend{ch: ch}
}

func (b *testBackend) Close() error { return nil }
func (b *testBackend) Send(m *LogMessage) error {
	b.ch <- m
	return nil
}

func TestLogForwarder(t *testing.T) {
	backend := newTestBackend(nil)
	dest := NewLogDestination("testdest", backend)
	defer dest.Close()
	destsFunc := func(string) ([]*LogDestination, error) { return []*LogDestination{dest}, nil }
	serviceName := "foo"
	fwd := NewLogForwarder(destsFunc, serviceName)

	text := "hello"
	_, err := io.WriteString(fwd, text)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case msg := <-backend.ch:
		if got := string(msg.Message); got != text {
			t.Errorf("incorrect message forwarded: want %q, got %q", text, got)
		}
		zero := time.Time{}
		if msg.Timestamp == zero {
			t.Errorf("forwarder failed to insert a timestamp")
		}
		if msg.Service != serviceName {
			t.Errorf("message sent with wrong service: want %q, got %q", serviceName, msg.Service)
		}
	case <-time.After(200 * time.Millisecond):
		t.Error("failed to receive forwarded result in time")
	}
}
