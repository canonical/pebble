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
	"encoding/json"
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
	const repeatAfter = 50 * time.Millisecond

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	start := time.Now()
	st.AddNotice(state.NoticeClient, "foo.com/bar", nil, repeatAfter)
	time.Sleep(time.Microsecond)
	st.AddNotice(state.NoticeClient, "foo.com/bar", nil, repeatAfter)

	notices := st.Notices(state.NoticeFilters{})
	c.Assert(notices, HasLen, 1)
	n := noticeToMap(c, notices[0])
	firstOccurred, err := time.Parse(time.RFC3339, n["first-occurred"].(string))
	c.Assert(err, IsNil)
	lastRepeated, err := time.Parse(time.RFC3339, n["last-repeated"].(string))
	c.Assert(err, IsNil)

	// LastRepeated won't yet be updated as we only waited 1us (repeat-after is 50ms)
	c.Assert(lastRepeated.Equal(firstOccurred), Equals, true)

	// Wait till last-repeated updates (should only be a few iterations)
	for {
		time.Sleep(10 * time.Millisecond)
		st.AddNotice(state.NoticeClient, "foo.com/bar", nil, repeatAfter)
		notices = st.Notices(state.NoticeFilters{})
		c.Assert(notices, HasLen, 1)
		n = noticeToMap(c, notices[0])
		newLastRepeated, err := time.Parse(time.RFC3339, n["last-repeated"].(string))
		c.Assert(err, IsNil)
		if newLastRepeated.After(lastRepeated) {
			break
		}
		if time.Since(start) > time.Second {
			c.Fatalf("timed out waiting for notice to repeat")
		}
	}
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
