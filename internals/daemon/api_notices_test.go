// Copyright (c) 2023-2024 Canonical Ltd
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
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/canonical/tc"

	"github.com/canonical/pebble/internals/overlord/identities"
	"github.com/canonical/pebble/internals/overlord/state"
)

func (s *apiSuite) TestNoticesFilterUserID(c *tc.C) {
	// A bit hacky... filter by user ID which doesn't have any notices to just
	// get public notices (those with nil user ID)
	s.testNoticesFilter(c, func(after time.Time) url.Values {
		return url.Values{"user-id": {"1000"}}
	})
}

func (s *apiSuite) TestNoticesFilterType(c *tc.C) {
	s.testNoticesFilter(c, func(after time.Time) url.Values {
		return url.Values{"types": {"custom"}}
	})
}

func (s *apiSuite) TestNoticesFilterKey(c *tc.C) {
	s.testNoticesFilter(c, func(after time.Time) url.Values {
		return url.Values{"keys": {"a.b/2"}}
	})
}

func (s *apiSuite) TestNoticesFilterAfter(c *tc.C) {
	s.testNoticesFilter(c, func(after time.Time) url.Values {
		return url.Values{"after": {after.UTC().Format(time.RFC3339Nano)}}
	})
}

func (s *apiSuite) TestNoticesFilterAll(c *tc.C) {
	s.testNoticesFilter(c, func(after time.Time) url.Values {
		return url.Values{
			"user-id": {"1000"},
			"types":   {"custom"},
			"keys":    {"a.b/2"},
			"after":   {after.UTC().Format(time.RFC3339Nano)},
		}
	})
}

func (s *apiSuite) testNoticesFilter(c *tc.C, makeQuery func(after time.Time) url.Values) {
	s.daemon(c)

	st := s.d.overlord.State()
	st.Lock()
	uid := uint32(123)
	addNotice(c, st, &uid, state.WarningNotice, "warning", nil)
	after := time.Now()
	time.Sleep(time.Microsecond)
	noticeID, err := st.AddNotice(nil, state.CustomNotice, "a.b/2", &state.AddNoticeOptions{
		Data: map[string]string{"k": "v"},
	})
	c.Assert(err, tc.ErrorIsNil)
	st.Unlock()

	query := makeQuery(after)
	req, err := http.NewRequest("GET", "/v1/notices?"+query.Encode(), nil)
	c.Assert(err, tc.ErrorIsNil)
	noticesCmd := apiCmd("/v1/notices")
	rsp, ok := noticesCmd.GET(noticesCmd, req, userState(identities.AdminAccess, 0)).(*resp)
	c.Assert(ok, tc.Equals, true)

	c.Check(rsp.Type, tc.Equals, ResponseTypeSync)
	c.Check(rsp.Status, tc.Equals, http.StatusOK)
	notices, ok := rsp.Result.([]*state.Notice)
	c.Assert(ok, tc.Equals, true)
	c.Assert(notices, tc.HasLen, 1)
	n := noticeToMap(c, notices[0])

	firstOccurred, err := time.Parse(time.RFC3339, n["first-occurred"].(string))
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(firstOccurred.After(after), tc.Equals, true)
	lastOccurred, err := time.Parse(time.RFC3339, n["last-occurred"].(string))
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(lastOccurred.Equal(firstOccurred), tc.Equals, true)
	lastRepeated, err := time.Parse(time.RFC3339, n["last-repeated"].(string))
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(lastRepeated.Equal(firstOccurred), tc.Equals, true)

	delete(n, "first-occurred")
	delete(n, "last-occurred")
	delete(n, "last-repeated")
	c.Assert(n, tc.DeepEquals, map[string]any{
		"id":           noticeID,
		"user-id":      nil,
		"type":         "custom",
		"key":          "a.b/2",
		"occurrences":  1.0,
		"last-data":    map[string]any{"k": "v"},
		"expire-after": "168h0m0s",
	})
}

func (s *apiSuite) TestNoticesFilterMultipleTypes(c *tc.C) {
	s.daemon(c)

	st := s.d.overlord.State()
	st.Lock()
	addNotice(c, st, nil, state.ChangeUpdateNotice, "123", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, st, nil, state.CustomNotice, "a.b/x", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, st, nil, state.WarningNotice, "danger", nil)
	st.Unlock()

	req, err := http.NewRequest("GET", "/v1/notices?types=change-update&types=warning,warning", nil)
	c.Assert(err, tc.ErrorIsNil)
	noticesCmd := apiCmd("/v1/notices")
	rsp, ok := noticesCmd.GET(noticesCmd, req, userState(identities.ReadAccess, 1000)).(*resp)
	c.Assert(ok, tc.Equals, true)

	c.Check(rsp.Type, tc.Equals, ResponseTypeSync)
	c.Check(rsp.Status, tc.Equals, http.StatusOK)
	notices, ok := rsp.Result.([]*state.Notice)
	c.Assert(ok, tc.Equals, true)
	c.Assert(notices, tc.HasLen, 2)
	n := noticeToMap(c, notices[0])
	c.Assert(n["type"], tc.Equals, "change-update")
	n = noticeToMap(c, notices[1])
	c.Assert(n["type"], tc.Equals, "warning")
}

func (s *apiSuite) TestNoticesFilterMultipleKeys(c *tc.C) {
	s.daemon(c)

	st := s.d.overlord.State()
	st.Lock()
	addNotice(c, st, nil, state.ChangeUpdateNotice, "123", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, st, nil, state.CustomNotice, "a.b/x", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, st, nil, state.WarningNotice, "danger", nil)
	st.Unlock()

	req, err := http.NewRequest("GET", "/v1/notices?keys=a.b/x&keys=danger", nil)
	c.Assert(err, tc.ErrorIsNil)
	noticesCmd := apiCmd("/v1/notices")
	rsp, ok := noticesCmd.GET(noticesCmd, req, userState(identities.ReadAccess, 1000)).(*resp)
	c.Assert(ok, tc.Equals, true)

	c.Check(rsp.Type, tc.Equals, ResponseTypeSync)
	c.Check(rsp.Status, tc.Equals, http.StatusOK)
	notices, ok := rsp.Result.([]*state.Notice)
	c.Assert(ok, tc.Equals, true)
	c.Assert(notices, tc.HasLen, 2)
	n := noticeToMap(c, notices[0])
	c.Assert(n["key"], tc.Equals, "a.b/x")
	n = noticeToMap(c, notices[1])
	c.Assert(n["key"], tc.Equals, "danger")
}

func (s *apiSuite) TestNoticesFilterInvalidTypes(c *tc.C) {
	s.daemon(c)

	st := s.d.Overlord().State()
	st.Lock()
	addNotice(c, st, nil, state.ChangeUpdateNotice, "123", nil)
	addNotice(c, st, nil, state.WarningNotice, "danger", nil)
	st.Unlock()

	// Check that invalid types are discarded, and notices with remaining
	// types are requested as expected, without error.
	req, err := http.NewRequest("GET", "/v1/notices?types=foo&types=warning&types=bar,baz", nil)
	c.Assert(err, tc.ErrorIsNil)
	noticesCmd := apiCmd("/v1/notices")
	rsp, ok := noticesCmd.GET(noticesCmd, req, userState(identities.ReadAccess, 1000)).(*resp)
	c.Assert(ok, tc.Equals, true)

	c.Check(rsp.Type, tc.Equals, ResponseTypeSync)
	c.Check(rsp.Status, tc.Equals, http.StatusOK)
	notices, ok := rsp.Result.([]*state.Notice)
	c.Assert(ok, tc.Equals, true)
	c.Assert(notices, tc.HasLen, 1)
	n := noticeToMap(c, notices[0])
	c.Assert(n["type"], tc.Equals, "warning")

	// Check that if all types are invalid, no notices are returned, and there
	// is no error.
	req, err = http.NewRequest("GET", "/v1/notices?types=foo&types=bar,baz", nil)
	c.Assert(err, tc.ErrorIsNil)
	noticesCmd = apiCmd("/v1/notices")
	rsp, ok = noticesCmd.GET(noticesCmd, req, userState(identities.ReadAccess, 1000)).(*resp)
	c.Assert(ok, tc.Equals, true)

	c.Check(rsp.Type, tc.Equals, ResponseTypeSync)
	c.Check(rsp.Status, tc.Equals, http.StatusOK)
	notices, ok = rsp.Result.([]*state.Notice)
	c.Assert(ok, tc.Equals, true)
	c.Assert(notices, tc.HasLen, 0)
}

func (s *apiSuite) TestNoticesUserIDAdminDefault(c *tc.C) {
	s.daemon(c)

	st := s.d.overlord.State()
	st.Lock()
	admin := uint32(0)
	nonAdmin := uint32(1000)
	otherNonAdmin := uint32(123)
	addNotice(c, st, &admin, state.ChangeUpdateNotice, "123", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, st, &nonAdmin, state.CustomNotice, "a.b/x", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, st, &otherNonAdmin, state.CustomNotice, "a.b/y", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, st, nil, state.WarningNotice, "danger", nil)
	st.Unlock()

	noticesCmd := apiCmd("/v1/notices")

	// Test that admin user sees their own and all public notices if no filter is specified
	req, err := http.NewRequest("GET", "/v1/notices", nil)
	c.Assert(err, tc.ErrorIsNil)
	rsp, ok := noticesCmd.GET(noticesCmd, req, userState(identities.AdminAccess, 0)).(*resp)
	c.Assert(ok, tc.Equals, true)

	c.Check(rsp.Type, tc.Equals, ResponseTypeSync)
	c.Check(rsp.Status, tc.Equals, http.StatusOK)
	notices, ok := rsp.Result.([]*state.Notice)
	c.Assert(ok, tc.Equals, true)
	c.Assert(notices, tc.HasLen, 2)
	n := noticeToMap(c, notices[0])
	c.Assert(n["user-id"], tc.Equals, float64(admin))
	c.Assert(n["key"], tc.Equals, "123")
	n = noticeToMap(c, notices[1])
	c.Assert(n["user-id"], tc.Equals, nil)
	c.Assert(n["key"], tc.Equals, "danger")
}

func (s *apiSuite) TestNoticesUserIDAdminFilter(c *tc.C) {
	s.daemon(c)

	st := s.d.overlord.State()
	st.Lock()
	admin := uint32(0)
	nonAdmin := uint32(1000)
	otherNonAdmin := uint32(123)
	addNotice(c, st, &admin, state.ChangeUpdateNotice, "123", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, st, &nonAdmin, state.CustomNotice, "a.b/x", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, st, &otherNonAdmin, state.CustomNotice, "a.b/y", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, st, nil, state.WarningNotice, "danger", nil)
	st.Unlock()

	noticesCmd := apiCmd("/v1/notices")

	// Test that admin can filter on any user ID, and always gets public notices too
	for _, uid := range []uint32{0, 1000, 123} {
		userIDValues := url.Values{}
		userIDValues.Add("user-id", strconv.FormatUint(uint64(uid), 10))
		reqUrl := fmt.Sprintf("/v1/notices?%s", userIDValues.Encode())
		req, err := http.NewRequest("GET", reqUrl, nil)
		c.Assert(err, tc.ErrorIsNil)
		rsp, ok := noticesCmd.GET(noticesCmd, req, userState(identities.AdminAccess, 0)).(*resp)
		c.Assert(ok, tc.Equals, true)

		c.Check(rsp.Type, tc.Equals, ResponseTypeSync)
		c.Check(rsp.Status, tc.Equals, http.StatusOK)
		notices, ok := rsp.Result.([]*state.Notice)
		c.Assert(ok, tc.Equals, true)
		c.Assert(notices, tc.HasLen, 2)
		n := noticeToMap(c, notices[0])
		c.Assert(n["user-id"], tc.Equals, float64(uid))
		n = noticeToMap(c, notices[1])
		c.Assert(n["user-id"], tc.Equals, nil)
	}
}

func (s *apiSuite) TestNoticesUserIDNonAdminDefault(c *tc.C) {
	s.daemon(c)

	st := s.d.overlord.State()
	st.Lock()
	admin := uint32(0)
	nonAdmin := uint32(1000)
	otherNonAdmin := uint32(123)
	addNotice(c, st, &admin, state.ChangeUpdateNotice, "123", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, st, &nonAdmin, state.CustomNotice, "a.b/x", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, st, &otherNonAdmin, state.CustomNotice, "a.b/y", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, st, nil, state.WarningNotice, "danger", nil)
	st.Unlock()

	noticesCmd := apiCmd("/v1/notices")

	// Test that non-admin user by default only sees their notices and public notices.
	req, err := http.NewRequest("GET", "/v1/notices", nil)
	c.Assert(err, tc.ErrorIsNil)
	rsp, ok := noticesCmd.GET(noticesCmd, req, userState(identities.ReadAccess, 1000)).(*resp)
	c.Assert(ok, tc.Equals, true)

	c.Check(rsp.Type, tc.Equals, ResponseTypeSync)
	c.Check(rsp.Status, tc.Equals, http.StatusOK)
	notices, ok := rsp.Result.([]*state.Notice)
	c.Assert(ok, tc.Equals, true)
	c.Assert(notices, tc.HasLen, 2)
	n := noticeToMap(c, notices[0])
	c.Assert(n["user-id"], tc.Equals, float64(nonAdmin))
	c.Assert(n["key"], tc.Equals, "a.b/x")
	n = noticeToMap(c, notices[1])
	c.Assert(n["user-id"], tc.Equals, nil)
	c.Assert(n["key"], tc.Equals, "danger")
}

func (s *apiSuite) TestNoticesUserIDNonAdminFilter(c *tc.C) {
	s.daemon(c)

	st := s.d.overlord.State()
	st.Lock()
	nonAdmin := uint32(1000)
	addNotice(c, st, &nonAdmin, state.CustomNotice, "a.b/x", nil)
	st.Unlock()

	noticesCmd := apiCmd("/v1/notices")

	// Test that non-admin user may not use --user-id filter
	reqUrl := "/v1/notices?user-id=1000"
	req, err := http.NewRequest("GET", reqUrl, nil)
	c.Assert(err, tc.ErrorIsNil)
	rsp, ok := noticesCmd.GET(noticesCmd, req, userState(identities.ReadAccess, 1000)).(*resp)
	c.Assert(ok, tc.Equals, true)

	c.Check(rsp.Type, tc.Equals, ResponseTypeError)
	c.Check(rsp.Status, tc.Equals, http.StatusForbidden)
	_, ok = rsp.Result.(*errorResult)
	c.Assert(ok, tc.Equals, true)
}

func (s *apiSuite) TestNoticesUsersAdminFilter(c *tc.C) {
	s.daemon(c)

	st := s.d.overlord.State()
	st.Lock()
	admin := uint32(0)
	nonAdmin := uint32(1000)
	otherNonAdmin := uint32(123)
	addNotice(c, st, &admin, state.ChangeUpdateNotice, "123", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, st, &nonAdmin, state.CustomNotice, "a.b/x", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, st, &otherNonAdmin, state.CustomNotice, "a.b/y", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, st, nil, state.WarningNotice, "danger", nil)
	st.Unlock()

	noticesCmd := apiCmd("/v1/notices")

	// Test that admin user may get all notices with --users=all filter
	reqUrl := "/v1/notices?users=all"
	req, err := http.NewRequest("GET", reqUrl, nil)
	c.Assert(err, tc.ErrorIsNil)
	rsp, ok := noticesCmd.GET(noticesCmd, req, userState(identities.AdminAccess, 0)).(*resp)
	c.Assert(ok, tc.Equals, true)

	c.Check(rsp.Type, tc.Equals, ResponseTypeSync)
	c.Check(rsp.Status, tc.Equals, http.StatusOK)
	notices, ok := rsp.Result.([]*state.Notice)
	c.Assert(ok, tc.Equals, true)
	c.Assert(notices, tc.HasLen, 4)
	n := noticeToMap(c, notices[0])
	c.Assert(n["user-id"], tc.Equals, float64(admin))
	c.Assert(n["key"], tc.Equals, "123")
	n = noticeToMap(c, notices[1])
	c.Assert(n["user-id"], tc.Equals, float64(nonAdmin))
	c.Assert(n["key"], tc.Equals, "a.b/x")
	n = noticeToMap(c, notices[2])
	c.Assert(n["user-id"], tc.Equals, float64(otherNonAdmin))
	c.Assert(n["key"], tc.Equals, "a.b/y")
	n = noticeToMap(c, notices[3])
	c.Assert(n["user-id"], tc.Equals, nil)
	c.Assert(n["key"], tc.Equals, "danger")
}

func (s *apiSuite) TestNoticesUsersNonAdminFilter(c *tc.C) {
	s.daemon(c)

	st := s.d.Overlord().State()
	st.Lock()
	nonAdmin := uint32(1000)
	addNotice(c, st, &nonAdmin, state.WarningNotice, "error1", nil)
	st.Unlock()

	noticesCmd := apiCmd("/v1/notices")

	// Test that non-admin user may not use --users filter
	reqUrl := "/v2/notices?users=all"
	req, err := http.NewRequest("GET", reqUrl, nil)
	c.Assert(err, tc.ErrorIsNil)
	rsp, ok := noticesCmd.GET(noticesCmd, req, userState(identities.ReadAccess, 1000)).(*resp)
	c.Assert(ok, tc.Equals, true)

	c.Check(rsp.Type, tc.Equals, ResponseTypeError)
	c.Check(rsp.Status, tc.Equals, http.StatusForbidden)
	_, ok = rsp.Result.(*errorResult)
	c.Assert(ok, tc.Equals, true)
}

func (s *apiSuite) TestNoticesUnknownRequestUID(c *tc.C) {
	s.daemon(c)

	st := s.d.overlord.State()
	st.Lock()
	addNotice(c, st, nil, state.WarningNotice, "danger", nil)
	st.Unlock()

	noticesCmd := apiCmd("/v1/notices")

	// Test that a connection with unknown UID is forbidden from receiving notices
	req, err := http.NewRequest("GET", "/v1/notices", nil)
	c.Assert(err, tc.ErrorIsNil)
	rsp, ok := noticesCmd.GET(noticesCmd, req, &UserState{Access: identities.ReadAccess}).(*resp)
	c.Assert(ok, tc.Equals, true)

	c.Check(rsp.Type, tc.Equals, ResponseTypeError)
	c.Check(rsp.Status, tc.Equals, http.StatusForbidden)
	_, ok = rsp.Result.(*errorResult)
	c.Assert(ok, tc.Equals, true)
}

func (s *apiSuite) TestNoticesWait(c *tc.C) {
	s.daemon(c)

	st := s.d.overlord.State()
	go func() {
		time.Sleep(10 * time.Millisecond)
		st.Lock()
		addNotice(c, st, nil, state.CustomNotice, "a.b/1", nil)
		st.Unlock()
	}()

	req, err := http.NewRequest("GET", "/v1/notices?timeout=1s", nil)
	c.Assert(err, tc.ErrorIsNil)
	noticesCmd := apiCmd("/v1/notices")
	rsp, ok := noticesCmd.GET(noticesCmd, req, userState(identities.ReadAccess, 1000)).(*resp)
	c.Assert(ok, tc.Equals, true)

	c.Check(rsp.Type, tc.Equals, ResponseTypeSync)
	c.Check(rsp.Status, tc.Equals, http.StatusOK)
	notices, ok := rsp.Result.([]*state.Notice)
	c.Assert(ok, tc.Equals, true)
	c.Assert(notices, tc.HasLen, 1)
	n := noticeToMap(c, notices[0])
	c.Check(n["user-id"], tc.Equals, nil)
	c.Check(n["type"], tc.Equals, "custom")
	c.Check(n["key"], tc.Equals, "a.b/1")
}

func (s *apiSuite) TestNoticesTimeout(c *tc.C) {
	s.daemon(c)

	req, err := http.NewRequest("GET", "/v1/notices?timeout=1ms", nil)
	c.Assert(err, tc.ErrorIsNil)
	noticesCmd := apiCmd("/v1/notices")
	rsp, ok := noticesCmd.GET(noticesCmd, req, userState(identities.ReadAccess, 1000)).(*resp)
	c.Assert(ok, tc.Equals, true)

	c.Check(rsp.Type, tc.Equals, ResponseTypeSync)
	c.Check(rsp.Status, tc.Equals, http.StatusOK)
	notices, ok := rsp.Result.([]*state.Notice)
	c.Assert(ok, tc.Equals, true)
	c.Assert(notices, tc.HasLen, 0)
}

func (s *apiSuite) TestNoticesRequestCancelled(c *tc.C) {
	s.daemon(c)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, "GET", "/v1/notices?timeout=1s", nil)
	c.Assert(err, tc.ErrorIsNil)
	noticesCmd := apiCmd("/v1/notices")
	rsp, ok := noticesCmd.GET(noticesCmd, req, userState(identities.ReadAccess, 1000)).(*resp)
	c.Assert(ok, tc.Equals, true)

	c.Check(rsp.Type, tc.Equals, ResponseTypeError)
	c.Check(rsp.Status, tc.Equals, http.StatusBadRequest)
	result, ok := rsp.Result.(*errorResult)
	c.Assert(ok, tc.Equals, true)
	c.Check(result.Message, tc.Matches, "request canceled")

	elapsed := time.Since(start)
	c.Check(elapsed > 10*time.Millisecond, tc.Equals, true)
	c.Check(elapsed < time.Second, tc.Equals, true)
}

func (s *apiSuite) TestNoticesInvalidUserID(c *tc.C) {
	s.testNoticesBadRequest(c, "user-id=foo", `invalid "user-id" filter:.*`)
}

func (s *apiSuite) TestNoticesInvalidUserIDMultiple(c *tc.C) {
	s.testNoticesBadRequest(c, "user-id=1000&user-id=1234", `invalid "user-id" filter:.*`)
}

func (s *apiSuite) TestNoticesInvalidUserIDHigh(c *tc.C) {
	s.testNoticesBadRequest(c, "user-id=4294967296", `invalid "user-id" filter:.*`)
}

func (s *apiSuite) TestNoticesInvalidUserIDLow(c *tc.C) {
	s.testNoticesBadRequest(c, "user-id=-1", `invalid "user-id" filter:.*`)
}

func (s *apiSuite) TestNoticesInvalidUsers(c *tc.C) {
	s.testNoticesBadRequest(c, "users=foo", `invalid "users" filter:.*`)
}

func (s *apiSuite) TestNoticesInvalidUserIDWithUsers(c *tc.C) {
	s.testNoticesBadRequest(c, "user-id=1234&users=all", `cannot use both "users" and "user-id" parameters`)
}

func (s *apiSuite) TestNoticesInvalidAfter(c *tc.C) {
	s.testNoticesBadRequest(c, "after=foo", `invalid "after" timestamp.*`)
}

func (s *apiSuite) TestNoticesInvalidTimeout(c *tc.C) {
	s.testNoticesBadRequest(c, "timeout=foo", "invalid timeout.*")
}

func (s *apiSuite) testNoticesBadRequest(c *tc.C, query, errorMatch string) {
	s.daemon(c)

	req, err := http.NewRequest("GET", "/v1/notices?"+query, nil)
	c.Assert(err, tc.ErrorIsNil)
	noticesCmd := apiCmd("/v1/notices")
	rsp, ok := noticesCmd.GET(noticesCmd, req, userState(identities.AdminAccess, 0)).(*resp)
	c.Assert(ok, tc.Equals, true)

	c.Check(rsp.Type, tc.Equals, ResponseTypeError)
	c.Check(rsp.Status, tc.Equals, http.StatusBadRequest)
	result, ok := rsp.Result.(*errorResult)
	c.Assert(ok, tc.Equals, true)
	c.Assert(result.Message, tc.Matches, errorMatch)
}

func (s *apiSuite) TestAddNotice(c *tc.C) {
	s.daemon(c)

	start := time.Now()
	body := []byte(`{
		"action": "add",
		"type": "custom",
		"key": "a.b/1",
		"repeat-after": "1h",
		"data": {"k": "v"}
	}`)
	req, err := http.NewRequest("POST", "/v1/notices", bytes.NewReader(body))
	c.Assert(err, tc.ErrorIsNil)
	noticesCmd := apiCmd("/v1/notices")
	rsp, ok := noticesCmd.POST(noticesCmd, req, userState(identities.ReadAccess, 1000)).(*resp)
	c.Assert(ok, tc.Equals, true)

	c.Check(rsp.Type, tc.Equals, ResponseTypeSync)
	c.Check(rsp.Status, tc.Equals, http.StatusOK)
	resultBytes, err := json.Marshal(rsp.Result)
	c.Assert(err, tc.ErrorIsNil)

	st := s.d.overlord.State()
	st.Lock()
	notices := st.Notices(nil)
	st.Unlock()
	c.Assert(notices, tc.HasLen, 1)
	n := noticeToMap(c, notices[0])
	noticeID, ok := n["id"].(string)
	c.Assert(ok, tc.Equals, true)
	c.Assert(string(resultBytes), tc.Equals, `{"id":"`+noticeID+`"}`)

	firstOccurred, err := time.Parse(time.RFC3339, n["first-occurred"].(string))
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(firstOccurred.After(start), tc.Equals, true)
	lastOccurred, err := time.Parse(time.RFC3339, n["last-occurred"].(string))
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(lastOccurred.Equal(firstOccurred), tc.Equals, true)
	lastRepeated, err := time.Parse(time.RFC3339, n["last-repeated"].(string))
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(lastRepeated.Equal(firstOccurred), tc.Equals, true)

	delete(n, "first-occurred")
	delete(n, "last-occurred")
	delete(n, "last-repeated")
	c.Assert(n, tc.DeepEquals, map[string]any{
		"id":           noticeID,
		"user-id":      1000.0,
		"type":         "custom",
		"key":          "a.b/1",
		"occurrences":  1.0,
		"last-data":    map[string]any{"k": "v"},
		"expire-after": "168h0m0s",
		"repeat-after": "1h0m0s",
	})
}

func (s *apiSuite) TestAddNoticeMinimal(c *tc.C) {
	s.daemon(c)

	body := []byte(`{
		"action": "add",
		"type": "custom",
		"key": "a.b/1"
	}`)
	req, err := http.NewRequest("POST", "/v1/notices", bytes.NewReader(body))
	c.Assert(err, tc.ErrorIsNil)
	noticesCmd := apiCmd("/v1/notices")
	rsp, ok := noticesCmd.POST(noticesCmd, req, userState(identities.ReadAccess, 1000)).(*resp)
	c.Assert(ok, tc.Equals, true)

	c.Check(rsp.Type, tc.Equals, ResponseTypeSync)
	c.Check(rsp.Status, tc.Equals, http.StatusOK)
	resultBytes, err := json.Marshal(rsp.Result)
	c.Assert(err, tc.ErrorIsNil)

	st := s.d.overlord.State()
	st.Lock()
	notices := st.Notices(nil)
	st.Unlock()
	c.Assert(notices, tc.HasLen, 1)
	n := noticeToMap(c, notices[0])
	noticeID, ok := n["id"].(string)
	c.Assert(ok, tc.Equals, true)
	c.Assert(string(resultBytes), tc.Equals, `{"id":"`+noticeID+`"}`)

	delete(n, "first-occurred")
	delete(n, "last-occurred")
	delete(n, "last-repeated")
	c.Assert(n, tc.DeepEquals, map[string]any{
		"id":           noticeID,
		"user-id":      1000.0,
		"type":         "custom",
		"key":          "a.b/1",
		"occurrences":  1.0,
		"expire-after": "168h0m0s",
	})
}

func (s *apiSuite) TestAddNoticeInvalidRequestUid(c *tc.C) {
	s.daemon(c)

	body := []byte(`{
		"action": "add",
		"type": "custom",
		"key": "a.b/1"
	}`)
	req, err := http.NewRequest("POST", "/v1/notices", bytes.NewReader(body))
	c.Assert(err, tc.ErrorIsNil)
	noticesCmd := apiCmd("/v1/notices")
	rsp, ok := noticesCmd.POST(noticesCmd, req, &UserState{Access: identities.ReadAccess}).(*resp)
	c.Assert(ok, tc.Equals, true)

	c.Check(rsp.Type, tc.Equals, ResponseTypeError)
	c.Check(rsp.Status, tc.Equals, http.StatusForbidden)
	_, ok = rsp.Result.(*errorResult)
	c.Assert(ok, tc.Equals, true)
}

func (s *apiSuite) TestAddNoticeInvalidAction(c *tc.C) {
	s.testAddNoticeBadRequest(c, `{"action": "bad"}`, "invalid action.*")
}

func (s *apiSuite) TestAddNoticeInvalidType(c *tc.C) {
	s.testAddNoticeBadRequest(c, `{"action": "add", "type": "foo"}`, "invalid type.*")
}

func (s *apiSuite) TestAddNoticeInvalidKey(c *tc.C) {
	s.testAddNoticeBadRequest(c, `{"action": "add", "type": "custom", "key": "bad"}`,
		"invalid key.*")
}

func (s *apiSuite) TestAddNoticeKeyTooLong(c *tc.C) {
	request, err := json.Marshal(map[string]any{
		"action": "add",
		"type":   "custom",
		"key":    "a.b/" + strings.Repeat("x", 257-4),
	})
	c.Assert(err, tc.ErrorIsNil)
	s.testAddNoticeBadRequest(c, string(request), "key must be 256 bytes or less")
}

func (s *apiSuite) TestAddNoticeDataTooLarge(c *tc.C) {
	request, err := json.Marshal(map[string]any{
		"action": "add",
		"type":   "custom",
		"key":    "a.b/c",
		"data": map[string]string{
			"a": strings.Repeat("x", 2047),
			"b": strings.Repeat("y", 2048),
		},
	})
	c.Assert(err, tc.ErrorIsNil)
	s.testAddNoticeBadRequest(c, string(request), "total size of data must be 4096 bytes or less")
}

func (s *apiSuite) TestAddNoticeInvalidRepeatAfter(c *tc.C) {
	s.testAddNoticeBadRequest(c, `{"action": "add", "type": "custom", "key": "a.b/1", "repeat-after": "bad"}`,
		"invalid repeat-after.*")
}

func (s *apiSuite) testAddNoticeBadRequest(c *tc.C, body, errorMatch string) {
	s.daemon(c)

	req, err := http.NewRequest("POST", "/v1/notices", strings.NewReader(body))
	c.Assert(err, tc.ErrorIsNil)
	noticesCmd := apiCmd("/v1/notices")
	rsp, ok := noticesCmd.POST(noticesCmd, req, userState(identities.ReadAccess, 1000)).(*resp)
	c.Assert(ok, tc.Equals, true)

	c.Check(rsp.Type, tc.Equals, ResponseTypeError)
	c.Check(rsp.Status, tc.Equals, http.StatusBadRequest)
	result, ok := rsp.Result.(*errorResult)
	c.Assert(ok, tc.Equals, true)
	c.Assert(result.Message, tc.Matches, errorMatch)
}

func (s *apiSuite) TestNotice(c *tc.C) {
	s.daemon(c)

	st := s.d.overlord.State()
	st.Lock()
	addNotice(c, st, nil, state.CustomNotice, "a.b/1", nil)
	noticeIDPublic, err := st.AddNotice(nil, state.CustomNotice, "a.b/2", nil)
	c.Assert(err, tc.ErrorIsNil)
	uid := uint32(1000)
	noticeIDPrivate, err := st.AddNotice(&uid, state.CustomNotice, "a.b/3", nil)
	c.Assert(err, tc.ErrorIsNil)
	addNotice(c, st, nil, state.CustomNotice, "a.b/4", nil)
	st.Unlock()

	req, err := http.NewRequest("GET", "/v1/notices/"+noticeIDPublic, nil)
	c.Assert(err, tc.ErrorIsNil)
	noticesCmd := apiCmd("/v1/notices/{id}")
	s.vars = map[string]string{"id": noticeIDPublic}
	rsp, ok := noticesCmd.GET(noticesCmd, req, userState(identities.ReadAccess, 1000)).(*resp)
	c.Assert(ok, tc.Equals, true)

	c.Check(rsp.Type, tc.Equals, ResponseTypeSync)
	c.Check(rsp.Status, tc.Equals, http.StatusOK)
	notice, ok := rsp.Result.(*state.Notice)
	c.Assert(ok, tc.Equals, true)
	n := noticeToMap(c, notice)
	c.Check(n["user-id"], tc.Equals, nil)
	c.Check(n["type"], tc.Equals, "custom")
	c.Check(n["key"], tc.Equals, "a.b/2")

	req, err = http.NewRequest("GET", "/v1/notices/"+noticeIDPrivate, nil)
	c.Assert(err, tc.ErrorIsNil)
	noticesCmd = apiCmd("/v1/notices/{id}")
	s.vars = map[string]string{"id": noticeIDPrivate}
	rsp, ok = noticesCmd.GET(noticesCmd, req, userState(identities.ReadAccess, 1000)).(*resp)
	c.Assert(ok, tc.Equals, true)

	c.Check(rsp.Type, tc.Equals, ResponseTypeSync)
	c.Check(rsp.Status, tc.Equals, http.StatusOK)
	notice, ok = rsp.Result.(*state.Notice)
	c.Assert(ok, tc.Equals, true)
	n = noticeToMap(c, notice)
	c.Check(n["user-id"], tc.Equals, 1000.0)
	c.Check(n["type"], tc.Equals, "custom")
	c.Check(n["key"], tc.Equals, "a.b/3")
}

func (s *apiSuite) TestNoticeNotFound(c *tc.C) {
	s.daemon(c)

	req, err := http.NewRequest("GET", "/v1/notices/1234", nil)
	c.Assert(err, tc.ErrorIsNil)
	noticesCmd := apiCmd("/v1/notices/{id}")
	s.vars = map[string]string{"id": "1234"}
	rsp, ok := noticesCmd.GET(noticesCmd, req, userState(identities.ReadAccess, 1000)).(*resp)
	c.Assert(ok, tc.Equals, true)

	c.Check(rsp.Type, tc.Equals, ResponseTypeError)
	c.Check(rsp.Status, tc.Equals, http.StatusNotFound)
	_, ok = rsp.Result.(*errorResult)
	c.Assert(ok, tc.Equals, true)
}

func (s *apiSuite) TestNoticeUnknownRequestUID(c *tc.C) {
	s.daemon(c)

	req, err := http.NewRequest("GET", "/v1/notices/1234", nil)
	c.Assert(err, tc.ErrorIsNil)
	noticesCmd := apiCmd("/v1/notices/{id}")
	s.vars = map[string]string{"id": "1234"}
	rsp, ok := noticesCmd.GET(noticesCmd, req, &UserState{Access: identities.ReadAccess}).(*resp)
	c.Assert(ok, tc.Equals, true)

	c.Check(rsp.Type, tc.Equals, ResponseTypeError)
	c.Check(rsp.Status, tc.Equals, http.StatusForbidden)
	_, ok = rsp.Result.(*errorResult)
	c.Assert(ok, tc.Equals, true)
}

func (s *apiSuite) TestNoticeAdminAllowed(c *tc.C) {
	s.daemon(c)

	st := s.d.overlord.State()
	st.Lock()
	uid := uint32(1000)
	noticeID, err := st.AddNotice(&uid, state.CustomNotice, "a.b/1", nil)
	c.Assert(err, tc.ErrorIsNil)
	st.Unlock()

	req, err := http.NewRequest("GET", "/v1/notices/"+noticeID, nil)
	c.Assert(err, tc.ErrorIsNil)
	noticesCmd := apiCmd("/v1/notices/{id}")
	s.vars = map[string]string{"id": noticeID}
	rsp, ok := noticesCmd.GET(noticesCmd, req, userState(identities.AdminAccess, 0)).(*resp)
	c.Assert(ok, tc.Equals, true)

	c.Check(rsp.Type, tc.Equals, ResponseTypeSync)
	c.Check(rsp.Status, tc.Equals, http.StatusOK)
	notice, ok := rsp.Result.(*state.Notice)
	c.Assert(ok, tc.Equals, true)
	n := noticeToMap(c, notice)
	c.Check(n["user-id"], tc.Equals, 1000.0)
	c.Check(n["type"], tc.Equals, "custom")
	c.Check(n["key"], tc.Equals, "a.b/1")
}

func (s *apiSuite) TestNoticeNonAdminNotAllowed(c *tc.C) {
	s.daemon(c)

	st := s.d.overlord.State()
	st.Lock()
	uid := uint32(1000)
	noticeID, err := st.AddNotice(&uid, state.CustomNotice, "a.b/1", nil)
	c.Assert(err, tc.ErrorIsNil)
	st.Unlock()

	req, err := http.NewRequest("GET", "/v1/notices/"+noticeID, nil)
	c.Assert(err, tc.ErrorIsNil)
	noticesCmd := apiCmd("/v1/notices/{id}")
	s.vars = map[string]string{"id": noticeID}
	rsp, ok := noticesCmd.GET(noticesCmd, req, userState(identities.ReadAccess, 1001)).(*resp)
	c.Assert(ok, tc.Equals, true)

	c.Check(rsp.Type, tc.Equals, ResponseTypeError)
	c.Check(rsp.Status, tc.Equals, http.StatusForbidden)
	_, ok = rsp.Result.(*errorResult)
	c.Assert(ok, tc.Equals, true)
}

func noticeToMap(c *tc.C, notice *state.Notice) map[string]any {
	buf, err := json.Marshal(notice)
	c.Assert(err, tc.ErrorIsNil)
	var n map[string]any
	err = json.Unmarshal(buf, &n)
	c.Assert(err, tc.ErrorIsNil)
	return n
}

func addNotice(c *tc.C, st *state.State, userID *uint32, noticeType state.NoticeType, key string, options *state.AddNoticeOptions) {
	_, err := st.AddNotice(userID, noticeType, key, options)
	c.Assert(err, tc.ErrorIsNil)
}

func userState(access identities.Access, uid uint32) *UserState {
	return &UserState{Access: access, UID: &uid}
}
