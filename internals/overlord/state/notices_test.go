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

package state_test

import (
	"bytes"
	"context"
	"encoding/json"
	"sync"
	"time"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/overlord/state"
)

type noticesSuite struct{}

var _ = Suite(&noticesSuite{})

func (s *noticesSuite) TestMarshal(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	start := time.Now()
	st.AddNotice(state.NoticeClient, "foo.com/bar", nil, 0)
	time.Sleep(time.Microsecond) // ensure there's time between the occurrences
	st.AddNotice(state.NoticeClient, "foo.com/bar", map[string]string{"k": "v"}, 0)

	notices := st.Notices(state.NoticeFilters{})
	c.Assert(notices, HasLen, 1)

	// Convert it to a map so we're not testing the JSON string directly
	// (order of fields doesn't matter).
	n := noticeToMap(c, notices[0])

	firstOccurred, err := time.Parse(time.RFC3339, n["first-occurred"].(string))
	c.Assert(err, IsNil)
	c.Assert(!firstOccurred.Before(start), Equals, true) // firstOccurred >= start
	lastOccurred, err := time.Parse(time.RFC3339, n["last-occurred"].(string))
	c.Assert(err, IsNil)
	c.Assert(lastOccurred.After(firstOccurred), Equals, true) // lastOccurred > firstOccurred
	lastRepeated, err := time.Parse(time.RFC3339, n["last-repeated"].(string))
	c.Assert(err, IsNil)
	c.Assert(lastRepeated.Equal(firstOccurred), Equals, true) // lastRepeated == firstOccurred

	delete(n, "first-occurred")
	delete(n, "last-occurred")
	delete(n, "last-repeated")
	c.Assert(n, DeepEquals, map[string]any{
		"id":           "1",
		"type":         "client",
		"key":          "foo.com/bar",
		"occurrences":  2.0,
		"last-data":    map[string]any{"k": "v"},
		"expire-after": "168h0m0s",
	})
}

func (s *noticesSuite) TestUnmarshal(c *C) {
	noticeJSON := []byte(`{
		"id": "1",
		"type": "client",
		"key": "foo.com/bar",
		"first-occurred": "2023-09-01T05:23:01Z",
		"last-occurred": "2023-09-01T07:23:02Z",
		"last-repeated": "2023-09-01T06:23:03.123456789Z",
		"occurrences": 2,
		"last-data": {"k": "v"},
		"repeat-after": "60m",
		"expire-after": "168h0m0s"
	}`)
	var notice *state.Notice
	err := json.Unmarshal(noticeJSON, &notice)
	c.Assert(err, IsNil)

	// The Notice fields aren't exported, so we need to marshal it into JSON
	// and then unmarshal it into a map to test.
	n := noticeToMap(c, notice)
	c.Assert(n, DeepEquals, map[string]any{
		"id":             "1",
		"type":           "client",
		"key":            "foo.com/bar",
		"first-occurred": "2023-09-01T05:23:01Z",
		"last-occurred":  "2023-09-01T07:23:02Z",
		"last-repeated":  "2023-09-01T06:23:03.123456789Z",
		"occurrences":    2.0,
		"last-data":      map[string]any{"k": "v"},
		"repeat-after":   "1h0m0s",
		"expire-after":   "168h0m0s",
	})
}

func (s *noticesSuite) TestOccurrences(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	st.AddNotice(state.NoticeClient, "foo.com/bar", nil, 0)
	st.AddNotice(state.NoticeClient, "foo.com/bar", nil, 0)
	st.AddNotice(state.NoticeClient, "foo.com/bar", nil, 0)
	time.Sleep(time.Microsecond)
	st.AddNotice(state.NoticeChangeUpdate, "123", nil, 0)

	notices := st.Notices(state.NoticeFilters{})
	c.Assert(notices, HasLen, 2)
	n := noticeToMap(c, notices[0])
	c.Assert(n["id"], Equals, "1")
	c.Assert(n["occurrences"], Equals, 3.0)
	n = noticeToMap(c, notices[1])
	c.Assert(n["id"], Equals, "2")
	c.Assert(n["occurrences"], Equals, 1.0)
}

func (s *noticesSuite) TestRepeatAfter(c *C) {
	const repeatAfter = 10 * time.Second

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	st.AddNotice(state.NoticeClient, "foo.com/bar", nil, repeatAfter)
	time.Sleep(time.Microsecond) // ensure there's time between the occurrences
	st.AddNotice(state.NoticeClient, "foo.com/bar", nil, repeatAfter)

	notices := st.Notices(state.NoticeFilters{})
	c.Assert(notices, HasLen, 1)
	n := noticeToMap(c, notices[0])
	firstOccurred, err := time.Parse(time.RFC3339, n["first-occurred"].(string))
	c.Assert(err, IsNil)
	lastRepeated, err := time.Parse(time.RFC3339, n["last-repeated"].(string))
	c.Assert(err, IsNil)

	// LastRepeated won't yet be updated as we only waited 1us (repeat-after is long)
	c.Assert(lastRepeated.Equal(firstOccurred), Equals, true)

	// Add a notice (with faked time) after a long time and ensure it has repeated
	future := time.Now().Add(repeatAfter)
	st.AddNoticeWithTime(future, state.NoticeClient, "foo.com/bar", nil, repeatAfter)
	notices = st.Notices(state.NoticeFilters{})
	c.Assert(notices, HasLen, 1)
	n = noticeToMap(c, notices[0])
	newLastRepeated, err := time.Parse(time.RFC3339, n["last-repeated"].(string))
	c.Assert(err, IsNil)
	c.Assert(newLastRepeated.After(lastRepeated), Equals, true)
}

func (s *noticesSuite) TestNoticesFilterType(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	st.AddNotice(state.NoticeClient, "foo.com/bar", nil, 0)
	st.AddNotice(state.NoticeChangeUpdate, "123", nil, 0)
	st.AddNotice(state.NoticeWarning, "Warning 1!", nil, 0)
	time.Sleep(time.Microsecond)
	st.AddNotice(state.NoticeWarning, "Warning 2!", nil, 0)

	notices := st.Notices(state.NoticeFilters{Type: state.NoticeWarning})
	c.Assert(notices, HasLen, 2)
	n := noticeToMap(c, notices[0])
	c.Assert(n["type"].(string), Equals, "warning")
	c.Assert(n["key"].(string), Equals, "Warning 1!")
	n = noticeToMap(c, notices[1])
	c.Assert(n["type"].(string), Equals, "warning")
	c.Assert(n["key"].(string), Equals, "Warning 2!")
}

func (s *noticesSuite) TestNoticesFilterKey(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	st.AddNotice(state.NoticeClient, "foo.com/bar", nil, 0)
	st.AddNotice(state.NoticeClient, "example.com/x", nil, 0)
	st.AddNotice(state.NoticeClient, "foo.com/baz", nil, 0)

	notices := st.Notices(state.NoticeFilters{Key: "example.com/x"})
	c.Assert(notices, HasLen, 1)
	n := noticeToMap(c, notices[0])
	c.Assert(n["type"].(string), Equals, "client")
	c.Assert(n["key"].(string), Equals, "example.com/x")
}

func (s *noticesSuite) TestNoticesFilterAfter(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	st.AddNotice(state.NoticeClient, "foo.com/x", nil, 0)
	notices := st.Notices(state.NoticeFilters{})
	c.Assert(notices, HasLen, 1)
	n := noticeToMap(c, notices[0])
	lastRepeated, err := time.Parse(time.RFC3339, n["last-repeated"].(string))
	c.Assert(err, IsNil)

	time.Sleep(time.Microsecond) // ensure there's time between the occurrences
	st.AddNotice(state.NoticeClient, "foo.com/y", nil, 0)

	notices = st.Notices(state.NoticeFilters{After: lastRepeated})
	c.Assert(notices, HasLen, 1)
	n = noticeToMap(c, notices[0])
	c.Assert(n["type"].(string), Equals, "client")
	c.Assert(n["key"].(string), Equals, "foo.com/y")
}

func (s *noticesSuite) TestNotice(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	st.AddNotice(state.NoticeClient, "foo.com/x", nil, 0)
	time.Sleep(time.Microsecond) // ensure there's time between the occurrences
	st.AddNotice(state.NoticeClient, "foo.com/y", nil, 0)
	time.Sleep(time.Microsecond) // ensure there's time between the occurrences
	st.AddNotice(state.NoticeClient, "foo.com/z", nil, 0)

	notices := st.Notices(state.NoticeFilters{})
	c.Assert(notices, HasLen, 3)
	n := noticeToMap(c, notices[1])
	noticeId := n["id"].(string)

	notice := st.Notice(noticeId)
	c.Assert(notice, NotNil)
	n = noticeToMap(c, notice)
	c.Assert(n["type"].(string), Equals, "client")
	c.Assert(n["key"].(string), Equals, "foo.com/y")
}

func (s *noticesSuite) TestEmptyState(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	notices := st.Notices(state.NoticeFilters{})
	c.Check(notices, HasLen, 0)
}

func (s *noticesSuite) TestCheckpoint(c *C) {
	backend := &fakeStateBackend{}
	st := state.New(backend)
	st.Lock()
	st.AddNotice(state.NoticeClient, "foo.com/bar", nil, 0)
	st.Unlock()
	c.Assert(backend.checkpoints, HasLen, 1)

	st2, err := state.ReadState(nil, bytes.NewReader(backend.checkpoints[0]))
	c.Assert(err, IsNil)
	st2.Lock()
	defer st2.Unlock()

	notices := st2.Notices(state.NoticeFilters{})
	c.Assert(notices, HasLen, 1)
	n := noticeToMap(c, notices[0])
	c.Assert(n["type"], Equals, "client")
	c.Assert(n["key"], Equals, "foo.com/bar")
}

func (s *noticesSuite) TestDeleteExpired(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	old := time.Now().Add(-8 * 24 * time.Hour)
	st.AddNoticeWithTime(old, state.NoticeClient, "foo.com/w", nil, 0)
	st.AddNoticeWithTime(old, state.NoticeClient, "foo.com/x", nil, 0)
	st.AddNotice(state.NoticeClient, "foo.com/y", nil, 0)
	time.Sleep(time.Microsecond) // ensure there's time between the occurrences
	st.AddNotice(state.NoticeClient, "foo.com/z", nil, 0)

	c.Assert(st.NumNotices(), Equals, 4)
	st.Prune(0, 0, 0)
	c.Assert(st.NumNotices(), Equals, 2)

	notices := st.Notices(state.NoticeFilters{})
	c.Assert(notices, HasLen, 2)
	n := noticeToMap(c, notices[0])
	c.Assert(n["key"], Equals, "foo.com/y")
	n = noticeToMap(c, notices[1])
	c.Assert(n["key"], Equals, "foo.com/z")
}

func (s *noticesSuite) TestWaitNoticesExisting(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	st.AddNotice(state.NoticeClient, "foo.com/bar", nil, 0)
	st.AddNotice(state.NoticeClient, "example.com/x", nil, 0)
	st.AddNotice(state.NoticeClient, "foo.com/baz", nil, 0)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	notices, err := st.WaitNotices(ctx, state.NoticeFilters{Key: "example.com/x"})
	c.Assert(err, IsNil)
	c.Assert(notices, HasLen, 1)
	n := noticeToMap(c, notices[0])
	c.Assert(n["type"].(string), Equals, "client")
	c.Assert(n["key"].(string), Equals, "example.com/x")
}

func (s *noticesSuite) TestWaitNoticesNew(c *C) {
	st := state.New(nil)

	go func() {
		time.Sleep(10 * time.Millisecond)
		st.Lock()
		defer st.Unlock()
		st.AddNotice(state.NoticeClient, "example.com/x", nil, 0)
		st.AddNotice(state.NoticeClient, "example.com/y", nil, 0)
	}()

	st.Lock()
	defer st.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	notices, err := st.WaitNotices(ctx, state.NoticeFilters{Key: "example.com/y"})
	c.Assert(err, IsNil)
	c.Assert(notices, HasLen, 1)
	n := noticeToMap(c, notices[0])
	c.Assert(n["key"].(string), Equals, "example.com/y")
}

// TODO: add test of WaitNotices timing out (the different cases)

// TODO: do this in a loop with concurrency 100 or so and short time.Sleep()s between each
func (s *noticesSuite) TestWaitNoticesMultipleWaiters(c *C) {
	st := state.New(nil)
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		st.Lock()
		defer st.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		notices, err := st.WaitNotices(ctx, state.NoticeFilters{Key: "example.com/x"})
		c.Assert(err, IsNil)
		c.Assert(notices, HasLen, 1)
		n := noticeToMap(c, notices[0])
		c.Assert(n["key"].(string), Equals, "example.com/x")
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		st.Lock()
		defer st.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		notices, err := st.WaitNotices(ctx, state.NoticeFilters{Key: "example.com/y"})
		c.Assert(err, IsNil)
		c.Assert(notices, HasLen, 1)
		n := noticeToMap(c, notices[0])
		c.Assert(n["key"].(string), Equals, "example.com/y")
	}()

	time.Sleep(10 * time.Millisecond)
	st.Lock()
	st.AddNotice(state.NoticeClient, "example.com/x", nil, 0)
	st.AddNotice(state.NoticeClient, "example.com/y", nil, 0)
	st.Unlock()

	// Wait for WaitNotice goroutines to finish
	wg.Wait()
}

// noticeToMap converts a Notice to a map using a JSON marshal-unmarshal round trip.
func noticeToMap(c *C, notice *state.Notice) map[string]any {
	buf, err := json.Marshal(notice)
	c.Assert(err, IsNil)
	var n map[string]any
	err = json.Unmarshal(buf, &n)
	c.Assert(err, IsNil)
	return n
}
