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

package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/overlord/state"
)

func (s *apiSuite) TestNoticesFilterType(c *C) {
	s.testNoticesFilter(c, func(after time.Time) url.Values {
		return url.Values{"type": {"client"}}
	})
}

func (s *apiSuite) TestNoticesFilterKey(c *C) {
	s.testNoticesFilter(c, func(after time.Time) url.Values {
		return url.Values{"key": {"a.b/2"}}
	})
}

func (s *apiSuite) TestNoticesFilterAfter(c *C) {
	s.testNoticesFilter(c, func(after time.Time) url.Values {
		return url.Values{"after": {after.UTC().Format(time.RFC3339Nano)}}
	})
}

func (s *apiSuite) TestNoticesFilterAll(c *C) {
	s.testNoticesFilter(c, func(after time.Time) url.Values {
		return url.Values{
			"type":  {"client"},
			"key":   {"a.b/2"},
			"after": {after.UTC().Format(time.RFC3339Nano)},
		}
	})
}

func (s *apiSuite) testNoticesFilter(c *C, makeQuery func(after time.Time) url.Values) {
	s.daemon(c)

	st := s.d.overlord.State()
	st.Lock()
	st.AddNotice(state.NoticeWarning, "warning", nil, 0)
	after := time.Now()
	time.Sleep(time.Microsecond)
	noticeId := st.AddNotice(state.NoticeClient, "a.b/2", map[string]string{"k": "v"}, 0)
	st.Unlock()

	query := makeQuery(after)
	req, err := http.NewRequest("GET", "/v1/notices?"+query.Encode(), nil)
	c.Assert(err, IsNil)
	noticesCmd := apiCmd("/v1/notices")
	rsp, ok := noticesCmd.GET(noticesCmd, req, nil).(*resp)
	c.Assert(ok, Equals, true)

	c.Check(rsp.Type, Equals, ResponseTypeSync)
	c.Check(rsp.Status, Equals, 200)
	notices, ok := rsp.Result.([]*state.Notice)
	c.Assert(ok, Equals, true)
	c.Assert(notices, HasLen, 1)
	n := noticeToMap(c, notices[0])

	firstOccurred, err := time.Parse(time.RFC3339, n["first-occurred"].(string))
	c.Assert(err, IsNil)
	c.Assert(firstOccurred.After(after), Equals, true)
	lastOccurred, err := time.Parse(time.RFC3339, n["last-occurred"].(string))
	c.Assert(err, IsNil)
	c.Assert(lastOccurred.Equal(firstOccurred), Equals, true)
	lastRepeated, err := time.Parse(time.RFC3339, n["last-repeated"].(string))
	c.Assert(err, IsNil)
	c.Assert(lastRepeated.Equal(firstOccurred), Equals, true)

	delete(n, "first-occurred")
	delete(n, "last-occurred")
	delete(n, "last-repeated")
	c.Assert(n, DeepEquals, map[string]any{
		"id":           noticeId,
		"type":         "client",
		"key":          "a.b/2",
		"occurrences":  1.0,
		"last-data":    map[string]any{"k": "v"},
		"expire-after": "168h0m0s",
	})
}

func (s *apiSuite) TestNoticesWait(c *C) {
	s.daemon(c)

	st := s.d.overlord.State()
	go func() {
		time.Sleep(10 * time.Millisecond)
		st.Lock()
		st.AddNotice(state.NoticeClient, "a.b/1", nil, 0)
		st.Unlock()
	}()

	req, err := http.NewRequest("GET", "/v1/notices?timeout=1s", nil)
	c.Assert(err, IsNil)
	noticesCmd := apiCmd("/v1/notices")
	rsp, ok := noticesCmd.GET(noticesCmd, req, nil).(*resp)
	c.Assert(ok, Equals, true)

	c.Check(rsp.Type, Equals, ResponseTypeSync)
	c.Check(rsp.Status, Equals, 200)
	notices, ok := rsp.Result.([]*state.Notice)
	c.Assert(ok, Equals, true)
	c.Assert(notices, HasLen, 1)
	n := noticeToMap(c, notices[0])
	c.Check(n["type"], Equals, "client")
	c.Check(n["key"], Equals, "a.b/1")
}

func (s *apiSuite) TestNoticesTimeout(c *C) {
	s.daemon(c)

	req, err := http.NewRequest("GET", "/v1/notices?timeout=1ms", nil)
	c.Assert(err, IsNil)
	noticesCmd := apiCmd("/v1/notices")
	rsp, ok := noticesCmd.GET(noticesCmd, req, nil).(*resp)
	c.Assert(ok, Equals, true)

	c.Check(rsp.Type, Equals, ResponseTypeError)
	c.Check(rsp.Status, Equals, http.StatusGatewayTimeout)
}

func (s *apiSuite) TestNoticesRequestCancelled(c *C) {
	s.daemon(c)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, "GET", "/v1/notices?timeout=1s", nil)
	c.Assert(err, IsNil)
	noticesCmd := apiCmd("/v1/notices")
	rsp, ok := noticesCmd.GET(noticesCmd, req, nil).(*resp)
	c.Assert(ok, Equals, true)

	c.Check(rsp.Type, Equals, ResponseTypeError)
	c.Check(rsp.Status, Equals, http.StatusBadRequest)

	elapsed := time.Since(start)
	c.Check(elapsed > 10*time.Millisecond, Equals, true)
	c.Check(elapsed < time.Second, Equals, true)
}

func (s *apiSuite) TestNoticesInvalidType(c *C) {
	s.testNoticesBadRequest(c, "type=foo", "invalid notice type.*")
}

func (s *apiSuite) TestNoticesInvalidAfter(c *C) {
	s.testNoticesBadRequest(c, "after=foo", "invalid after timestamp.*")
}

func (s *apiSuite) TestNoticesInvalidTimeout(c *C) {
	s.testNoticesBadRequest(c, "timeout=foo", "invalid timeout.*")
}

func (s *apiSuite) testNoticesBadRequest(c *C, query, errorMatch string) {
	s.daemon(c)

	req, err := http.NewRequest("GET", "/v1/notices?"+query, nil)
	c.Assert(err, IsNil)
	noticesCmd := apiCmd("/v1/notices")
	rsp, ok := noticesCmd.GET(noticesCmd, req, nil).(*resp)
	c.Assert(ok, Equals, true)

	c.Check(rsp.Type, Equals, ResponseTypeError)
	c.Check(rsp.Status, Equals, http.StatusBadRequest)

	result, ok := rsp.Result.(*errorResult)
	c.Assert(ok, Equals, true)
	c.Assert(result.Message, Matches, errorMatch)
}

func (s *apiSuite) TestAddNotice(c *C) {
	s.daemon(c)

	start := time.Now()
	body := []byte(`{
		"action": "add",
		"type": "client",
		"key": "a.b/1",
		"repeat-after": "1h",
		"data": {"k": "v"}
	}`)
	req, err := http.NewRequest("POST", "/v1/notices", bytes.NewReader(body))
	c.Assert(err, IsNil)
	noticesCmd := apiCmd("/v1/notices")
	rsp, ok := noticesCmd.POST(noticesCmd, req, nil).(*resp)
	c.Assert(ok, Equals, true)

	c.Check(rsp.Type, Equals, ResponseTypeSync)
	c.Check(rsp.Status, Equals, 200)
	resultBytes, err := json.Marshal(rsp.Result)
	c.Assert(err, IsNil)

	st := s.d.overlord.State()
	st.Lock()
	notices := st.Notices(state.NoticeFilters{})
	st.Unlock()
	c.Assert(notices, HasLen, 1)
	n := noticeToMap(c, notices[0])
	noticeId, ok := n["id"].(string)
	c.Assert(ok, Equals, true)
	c.Assert(string(resultBytes), Equals, `{"id":"`+noticeId+`"}`)

	firstOccurred, err := time.Parse(time.RFC3339, n["first-occurred"].(string))
	c.Assert(err, IsNil)
	c.Assert(firstOccurred.After(start), Equals, true)
	lastOccurred, err := time.Parse(time.RFC3339, n["last-occurred"].(string))
	c.Assert(err, IsNil)
	c.Assert(lastOccurred.Equal(firstOccurred), Equals, true)
	lastRepeated, err := time.Parse(time.RFC3339, n["last-repeated"].(string))
	c.Assert(err, IsNil)
	c.Assert(lastRepeated.Equal(firstOccurred), Equals, true)

	delete(n, "first-occurred")
	delete(n, "last-occurred")
	delete(n, "last-repeated")
	c.Assert(n, DeepEquals, map[string]any{
		"id":           noticeId,
		"type":         "client",
		"key":          "a.b/1",
		"occurrences":  1.0,
		"last-data":    map[string]any{"k": "v"},
		"expire-after": "168h0m0s",
		"repeat-after": "1h0m0s",
	})
}

func (s *apiSuite) TestAddNoticeInvalidAction(c *C) {
	s.testAddNoticeBadRequest(c, `{"action": "bad"}`, "invalid action.*")
}

func (s *apiSuite) TestAddNoticeInvalidType(c *C) {
	s.testAddNoticeBadRequest(c, `{"action": "add", "type": "foo"}`, "invalid type.*")
}

func (s *apiSuite) TestAddNoticeInvalidKey(c *C) {
	s.testAddNoticeBadRequest(c, `{"action": "add", "type": "client", "key": "bad"}`,
		"invalid key.*")
}

func (s *apiSuite) TestAddNoticeKeyTooLong(c *C) {
	key := "a.b/" + strings.Repeat("x", 1000)
	s.testAddNoticeBadRequest(c, `{"action": "add", "type": "client", "key": "`+key+`"}`,
		"key too long.*")
}

func (s *apiSuite) TestInvalidRepeatAfter(c *C) {
	s.testAddNoticeBadRequest(c, `{"action": "add", "type": "client", "key": "a.b/1", "repeat-after": "bad"}`,
		"invalid repeat-after.*")
}

func (s *apiSuite) testAddNoticeBadRequest(c *C, body, errorMatch string) {
	s.daemon(c)

	req, err := http.NewRequest("POST", "/v1/notices", strings.NewReader(body))
	c.Assert(err, IsNil)
	noticesCmd := apiCmd("/v1/notices")
	rsp, ok := noticesCmd.POST(noticesCmd, req, nil).(*resp)
	c.Assert(ok, Equals, true)

	c.Check(rsp.Type, Equals, ResponseTypeError)
	c.Check(rsp.Status, Equals, http.StatusBadRequest)

	result, ok := rsp.Result.(*errorResult)
	c.Assert(ok, Equals, true)
	c.Assert(result.Message, Matches, errorMatch)
}

func (s *apiSuite) TestNotice(c *C) {
	s.daemon(c)

	st := s.d.overlord.State()
	st.Lock()
	st.AddNotice(state.NoticeClient, "a.b/1", nil, 0)
	noticeId := st.AddNotice(state.NoticeClient, "a.b/2", nil, 0)
	st.AddNotice(state.NoticeClient, "a.b/3", nil, 0)
	st.Unlock()

	req, err := http.NewRequest("GET", "/v1/notices/"+noticeId, nil)
	c.Assert(err, IsNil)
	noticesCmd := apiCmd("/v1/notices/{id}")
	s.vars = map[string]string{"id": noticeId}
	rsp, ok := noticesCmd.GET(noticesCmd, req, nil).(*resp)
	c.Assert(ok, Equals, true)

	c.Check(rsp.Type, Equals, ResponseTypeSync)
	c.Check(rsp.Status, Equals, 200)
	notice, ok := rsp.Result.(*state.Notice)
	c.Assert(ok, Equals, true)
	n := noticeToMap(c, notice)
	c.Check(n["type"], Equals, "client")
	c.Check(n["key"], Equals, "a.b/2")
}

func (s *apiSuite) TestNoticeNotFound(c *C) {
	s.daemon(c)

	req, err := http.NewRequest("GET", "/v1/notices/1234", nil)
	c.Assert(err, IsNil)
	noticesCmd := apiCmd("/v1/notices/{id}")
	s.vars = map[string]string{"id": "1234"}
	rsp, ok := noticesCmd.GET(noticesCmd, req, nil).(*resp)
	c.Assert(ok, Equals, true)

	c.Check(rsp.Type, Equals, ResponseTypeError)
	c.Check(rsp.Status, Equals, 404)
}

func noticeToMap(c *C, notice *state.Notice) map[string]any {
	buf, err := json.Marshal(notice)
	c.Assert(err, IsNil)
	var n map[string]any
	err = json.Unmarshal(buf, &n)
	c.Assert(err, IsNil)
	return n
}