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
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/osutil/sys"
	"github.com/canonical/pebble/internals/overlord/state"
)

func mockSysGetuid(fakeUid uint32) (restore func()) {
	old := sysGetuid
	sysGetuid = func() sys.UserID {
		return sys.UserID(fakeUid)
	}
	restore = func() {
		sysGetuid = old
	}
	return restore
}

func (s *apiSuite) TestNoticesFilterUserID(c *C) {
	restore := mockSysGetuid(0)
	defer restore()
	s.testNoticesFilter(c, func(after time.Time) url.Values {
		return url.Values{"user-ids": {"1000"}}
	})
}

func (s *apiSuite) TestNoticesFilterType(c *C) {
	restore := mockSysGetuid(0)
	defer restore()
	s.testNoticesFilter(c, func(after time.Time) url.Values {
		return url.Values{"types": {"custom"}}
	})
}

func (s *apiSuite) TestNoticesFilterKey(c *C) {
	restore := mockSysGetuid(0)
	defer restore()
	s.testNoticesFilter(c, func(after time.Time) url.Values {
		return url.Values{"keys": {"a.b/2"}}
	})
}

func (s *apiSuite) TestNoticesFilterVisibility(c *C) {
	restore := mockSysGetuid(0)
	defer restore()
	s.testNoticesFilter(c, func(after time.Time) url.Values {
		return url.Values{"visibilities": {"public"}}
	})
}

func (s *apiSuite) TestNoticesFilterAfter(c *C) {
	restore := mockSysGetuid(0)
	defer restore()
	s.testNoticesFilter(c, func(after time.Time) url.Values {
		return url.Values{"after": {after.UTC().Format(time.RFC3339Nano)}}
	})
}

func (s *apiSuite) TestNoticesFilterAll(c *C) {
	restore := mockSysGetuid(0)
	defer restore()
	s.testNoticesFilter(c, func(after time.Time) url.Values {
		return url.Values{
			"user-ids":     {"1000"},
			"types":        {"custom"},
			"keys":         {"a.b/2"},
			"visibilities": {"public"},
			"after":        {after.UTC().Format(time.RFC3339Nano)},
		}
	})
}

func (s *apiSuite) testNoticesFilter(c *C, makeQuery func(after time.Time) url.Values) {
	s.daemon(c)

	st := s.d.overlord.State()
	st.Lock()
	addNotice(c, st, 0, state.WarningNotice, "warning", nil)
	after := time.Now()
	time.Sleep(time.Microsecond)
	noticeId, err := st.AddNotice(1000, state.CustomNotice, "a.b/2", &state.AddNoticeOptions{
		Visibility: state.PublicNotice,
		Data:       map[string]string{"k": "v"},
	})
	c.Assert(err, IsNil)
	st.Unlock()

	query := makeQuery(after)
	req, err := http.NewRequest("GET", "/v1/notices?"+query.Encode(), nil)
	req.RemoteAddr = "pid=100;uid=0;socket=;"
	c.Assert(err, IsNil)
	noticesCmd := apiCmd("/v1/notices")
	rsp, ok := noticesCmd.GET(noticesCmd, req, nil).(*resp)
	c.Assert(ok, Equals, true)

	c.Check(rsp.Type, Equals, ResponseTypeSync)
	c.Check(rsp.Status, Equals, http.StatusOK)
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
		"user-id":      1000.0,
		"type":         "custom",
		"key":          "a.b/2",
		"visibility":   "public",
		"occurrences":  1.0,
		"last-data":    map[string]any{"k": "v"},
		"expire-after": "168h0m0s",
	})
}

func (s *apiSuite) TestNoticesFilterMultipleTypes(c *C) {
	s.daemon(c)
	restore := mockSysGetuid(0)
	defer restore()

	st := s.d.overlord.State()
	st.Lock()
	addNotice(c, st, 1000, state.ChangeUpdateNotice, "123", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, st, 1000, state.CustomNotice, "a.b/x", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, st, 1000, state.WarningNotice, "danger", nil)
	st.Unlock()

	req, err := http.NewRequest("GET", "/v1/notices?types=change-update&types=warning", nil)
	req.RemoteAddr = "pid=100;uid=1000;socket=;"
	c.Assert(err, IsNil)
	noticesCmd := apiCmd("/v1/notices")
	rsp, ok := noticesCmd.GET(noticesCmd, req, nil).(*resp)
	c.Assert(ok, Equals, true)

	c.Check(rsp.Type, Equals, ResponseTypeSync)
	c.Check(rsp.Status, Equals, http.StatusOK)
	notices, ok := rsp.Result.([]*state.Notice)
	c.Assert(ok, Equals, true)
	c.Assert(notices, HasLen, 2)
	n := noticeToMap(c, notices[0])
	c.Assert(n["type"], Equals, "change-update")
	n = noticeToMap(c, notices[1])
	c.Assert(n["type"], Equals, "warning")
}

func (s *apiSuite) TestNoticesFilterMultipleKeys(c *C) {
	s.daemon(c)
	restore := mockSysGetuid(0)
	defer restore()

	st := s.d.overlord.State()
	st.Lock()
	addNotice(c, st, 1000, state.ChangeUpdateNotice, "123", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, st, 1000, state.CustomNotice, "a.b/x", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, st, 1000, state.WarningNotice, "danger", nil)
	st.Unlock()

	req, err := http.NewRequest("GET", "/v1/notices?keys=a.b/x&keys=danger", nil)
	req.RemoteAddr = "pid=100;uid=1000;socket=;"
	c.Assert(err, IsNil)
	noticesCmd := apiCmd("/v1/notices")
	rsp, ok := noticesCmd.GET(noticesCmd, req, nil).(*resp)
	c.Assert(ok, Equals, true)

	c.Check(rsp.Type, Equals, ResponseTypeSync)
	c.Check(rsp.Status, Equals, http.StatusOK)
	notices, ok := rsp.Result.([]*state.Notice)
	c.Assert(ok, Equals, true)
	c.Assert(notices, HasLen, 2)
	n := noticeToMap(c, notices[0])
	c.Assert(n["key"], Equals, "a.b/x")
	n = noticeToMap(c, notices[1])
	c.Assert(n["key"], Equals, "danger")
}

func (s *apiSuite) TestNoticesFilterInvalidTypes(c *C) {
	s.daemon(c)
	restore := mockSysGetuid(0)
	defer restore()

	st := s.d.Overlord().State()
	st.Lock()
	addNotice(c, st, 1000, state.ChangeUpdateNotice, "123", nil)
	addNotice(c, st, 1000, state.WarningNotice, "danger", nil)
	st.Unlock()

	// Check that invalid types are discarded, and notices with remaining
	// types are requested as expected, without error.
	req, err := http.NewRequest("GET", "/v1/notices?types=foo&types=warning&types=bar,baz", nil)
	req.RemoteAddr = "pid=100;uid=1000;socket=;"
	c.Assert(err, IsNil)
	noticesCmd := apiCmd("/v1/notices")
	rsp, ok := noticesCmd.GET(noticesCmd, req, nil).(*resp)
	c.Assert(ok, Equals, true)

	c.Check(rsp.Type, Equals, ResponseTypeSync)
	c.Check(rsp.Status, Equals, http.StatusOK)
	notices, ok := rsp.Result.([]*state.Notice)
	c.Assert(ok, Equals, true)
	c.Assert(notices, HasLen, 1)
	n := noticeToMap(c, notices[0])
	c.Assert(n["type"], Equals, "warning")

	// Check that if all types are invalid, no notices are returned, and there
	// is no error.
	req, err = http.NewRequest("GET", "/v1/notices?types=foo&types=bar,baz", nil)
	c.Assert(err, IsNil)
	noticesCmd = apiCmd("/v1/notices")
	rsp, ok = noticesCmd.GET(noticesCmd, req, nil).(*resp)
	c.Assert(ok, Equals, true)

	c.Check(rsp.Type, Equals, ResponseTypeSync)
	c.Check(rsp.Status, Equals, http.StatusOK)
	notices, ok = rsp.Result.([]*state.Notice)
	c.Assert(ok, Equals, true)
	c.Assert(notices, HasLen, 0)
}

func (s *apiSuite) TestNoticesUserIDsRootDefault(c *C) {
	s.daemon(c)
	restore := mockSysGetuid(0)
	defer restore()

	st := s.d.overlord.State()
	st.Lock()
	addNotice(c, st, 0, state.ChangeUpdateNotice, "123", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, st, 1000, state.CustomNotice, "a.b/x", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, st, 123, state.CustomNotice, "a.b/y", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, st, 0, state.WarningNotice, "danger", &state.AddNoticeOptions{
		Visibility: state.PublicNotice,
	})
	addNotice(c, st, 1000, state.ChangeUpdateNotice, "456", &state.AddNoticeOptions{
		Visibility: state.PublicNotice,
	})
	addNotice(c, st, 123, state.ChangeUpdateNotice, "789", &state.AddNoticeOptions{
		Visibility: state.PublicNotice,
	})
	st.Unlock()

	noticesCmd := apiCmd("/v1/notices")

	// Test that root user sees all notices if no filter is specified
	req, err := http.NewRequest("GET", "/v1/notices", nil)
	c.Assert(err, IsNil)
	req.RemoteAddr = "pid=100;uid=0;socket=;"
	rsp, ok := noticesCmd.GET(noticesCmd, req, nil).(*resp)
	c.Assert(ok, Equals, true)

	c.Check(rsp.Type, Equals, ResponseTypeSync)
	c.Check(rsp.Status, Equals, http.StatusOK)
	notices, ok := rsp.Result.([]*state.Notice)
	c.Assert(ok, Equals, true)
	c.Assert(notices, HasLen, 6)
	n := noticeToMap(c, notices[0])
	c.Assert(n["user-id"], Equals, 0.0)
	n = noticeToMap(c, notices[1])
	c.Assert(n["user-id"], Equals, 1000.0)
	n = noticeToMap(c, notices[2])
	c.Assert(n["user-id"], Equals, 123.0)
	n = noticeToMap(c, notices[3])
	c.Assert(n["user-id"], Equals, 0.0)
	n = noticeToMap(c, notices[4])
	c.Assert(n["user-id"], Equals, 1000.0)
	n = noticeToMap(c, notices[5])
	c.Assert(n["user-id"], Equals, 123.0)
}

func (s *apiSuite) TestNoticesUserIDsRootFilter(c *C) {
	s.daemon(c)
	restore := mockSysGetuid(0)
	defer restore()

	st := s.d.overlord.State()
	st.Lock()
	addNotice(c, st, 0, state.ChangeUpdateNotice, "123", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, st, 1000, state.CustomNotice, "a.b/x", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, st, 123, state.CustomNotice, "a.b/y", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, st, 0, state.WarningNotice, "danger", &state.AddNoticeOptions{
		Visibility: state.PublicNotice,
	})
	addNotice(c, st, 1000, state.ChangeUpdateNotice, "456", &state.AddNoticeOptions{
		Visibility: state.PublicNotice,
	})
	addNotice(c, st, 123, state.ChangeUpdateNotice, "789", &state.AddNoticeOptions{
		Visibility: state.PublicNotice,
	})
	st.Unlock()

	noticesCmd := apiCmd("/v1/notices")

	// Test that root can filter on any user IDs
	for _, testCase := range []struct {
		filterUserIDs   []uint32
		expectedUserIDs []float64
	}{
		{
			[]uint32{0},
			[]float64{0.0, 0.0},
		},
		{
			[]uint32{1000},
			[]float64{1000.0, 1000.0},
		},
		{
			[]uint32{123},
			[]float64{123.0, 123.0},
		},
		{
			[]uint32{0, 1000},
			[]float64{0.0, 1000.0, 0.0, 1000.0},
		},
		{
			[]uint32{0, 123},
			[]float64{0.0, 123.0, 0.0, 123.0},
		},
		{
			[]uint32{1000, 123},
			[]float64{1000.0, 123.0, 1000.0, 123.0},
		},
		{
			[]uint32{0, 1000, 123},
			[]float64{0.0, 1000.0, 123.0, 0.0, 1000.0, 123.0},
		},
	} {
		userIDsValues := url.Values{}
		for _, uid := range testCase.filterUserIDs {
			userIDsValues.Add("user-ids", strconv.FormatUint(uint64(uid), 10))
		}
		reqUrl := fmt.Sprintf("/v1/notices?%s", userIDsValues.Encode())
		req, err := http.NewRequest("GET", reqUrl, nil)
		c.Assert(err, IsNil)
		req.RemoteAddr = "pid=100;uid=0;socket=;"
		rsp, ok := noticesCmd.GET(noticesCmd, req, nil).(*resp)
		c.Assert(ok, Equals, true)

		c.Check(rsp.Type, Equals, ResponseTypeSync)
		c.Check(rsp.Status, Equals, http.StatusOK)
		notices, ok := rsp.Result.([]*state.Notice)
		c.Assert(ok, Equals, true)
		c.Assert(notices, HasLen, len(testCase.expectedUserIDs))
		for i, uid := range testCase.expectedUserIDs {
			n := noticeToMap(c, notices[i])
			c.Assert(n["user-id"], Equals, uid)
		}
	}
}

func (s *apiSuite) TestNoticesUserIDsNonRootDefault(c *C) {
	s.daemon(c)
	restore := mockSysGetuid(0)
	defer restore()

	st := s.d.overlord.State()
	st.Lock()
	addNotice(c, st, 0, state.ChangeUpdateNotice, "123", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, st, 1000, state.CustomNotice, "a.b/x", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, st, 123, state.CustomNotice, "a.b/y", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, st, 0, state.WarningNotice, "danger", &state.AddNoticeOptions{
		Visibility: state.PublicNotice,
	})
	addNotice(c, st, 1000, state.ChangeUpdateNotice, "456", &state.AddNoticeOptions{
		Visibility: state.PublicNotice,
	})
	addNotice(c, st, 123, state.ChangeUpdateNotice, "789", &state.AddNoticeOptions{
		Visibility: state.PublicNotice,
	})
	st.Unlock()

	noticesCmd := apiCmd("/v1/notices")

	// Test that non-root user by default only sees their notices and notices
	// intended for all user IDs (represented by UID of -1).
	req, err := http.NewRequest("GET", "/v1/notices", nil)
	c.Assert(err, IsNil)
	req.RemoteAddr = "pid=100;uid=1000;socket=;"
	rsp, ok := noticesCmd.GET(noticesCmd, req, nil).(*resp)
	c.Assert(ok, Equals, true)

	c.Check(rsp.Type, Equals, ResponseTypeSync)
	c.Check(rsp.Status, Equals, http.StatusOK)
	notices, ok := rsp.Result.([]*state.Notice)
	c.Assert(ok, Equals, true)
	c.Assert(notices, HasLen, 4)
	n := noticeToMap(c, notices[0])
	c.Assert(n["user-id"], Equals, 1000.0)
	n = noticeToMap(c, notices[1])
	c.Assert(n["user-id"], Equals, 0.0)
	n = noticeToMap(c, notices[2])
	c.Assert(n["user-id"], Equals, 1000.0)
	n = noticeToMap(c, notices[3])
	c.Assert(n["user-id"], Equals, 123.0)
}

func (s *apiSuite) TestNoticesUserIDsNonRootFilter(c *C) {
	s.daemon(c)
	restore := mockSysGetuid(0)
	defer restore()

	st := s.d.overlord.State()
	st.Lock()
	addNotice(c, st, 0, state.ChangeUpdateNotice, "123", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, st, 1000, state.CustomNotice, "a.b/x", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, st, 123, state.CustomNotice, "a.b/y", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, st, 0, state.WarningNotice, "danger", &state.AddNoticeOptions{
		Visibility: state.PublicNotice,
	})
	addNotice(c, st, 1000, state.ChangeUpdateNotice, "456", &state.AddNoticeOptions{
		Visibility: state.PublicNotice,
	})
	addNotice(c, st, 123, state.ChangeUpdateNotice, "789", &state.AddNoticeOptions{
		Visibility: state.PublicNotice,
	})
	st.Unlock()

	noticesCmd := apiCmd("/v1/notices")

	// Test that non-root user can only filter on their user ID and UID -1.
	for _, testCase := range []struct {
		filterUserIDs   []uint32
		expectedUserIDs []float64
	}{
		{
			[]uint32{0},
			[]float64{0.0},
		},
		{
			[]uint32{1000},
			[]float64{1000.0, 1000.0},
		},
		{
			[]uint32{123},
			[]float64{123.0},
		},
		{
			[]uint32{0, 1000},
			[]float64{1000.0, 0.0, 1000.0},
		},
		{
			[]uint32{0, 123},
			[]float64{0.0, 123.0},
		},
		{
			[]uint32{1000, 123},
			[]float64{1000.0, 1000.0, 123.0},
		},
		{
			[]uint32{0, 1000, 123},
			[]float64{1000.0, 0.0, 1000.0, 123.0},
		},
	} {
		userIDsValues := url.Values{}
		for _, uid := range testCase.filterUserIDs {
			userIDsValues.Add("user-ids", strconv.FormatUint(uint64(uid), 10))
		}
		reqUrl := fmt.Sprintf("/v1/notices?%s", userIDsValues.Encode())
		req, err := http.NewRequest("GET", reqUrl, nil)
		c.Assert(err, IsNil)
		req.RemoteAddr = "pid=100;uid=1000;socket=;"
		rsp, ok := noticesCmd.GET(noticesCmd, req, nil).(*resp)
		c.Assert(ok, Equals, true)

		c.Check(rsp.Type, Equals, ResponseTypeSync)
		c.Check(rsp.Status, Equals, http.StatusOK)
		notices, ok := rsp.Result.([]*state.Notice)
		c.Assert(ok, Equals, true)
		// Non-root filtering on UID other than or their own UID yields only
		// public notices for that UID.
		c.Assert(notices, HasLen, len(testCase.expectedUserIDs))
		for i, uid := range testCase.expectedUserIDs {
			n := noticeToMap(c, notices[i])
			c.Assert(n["user-id"], Equals, uid)
		}
	}
}

func (s *apiSuite) TestNoticesUnknownReqUid(c *C) {
	s.daemon(c)
	restore := mockSysGetuid(0)
	defer restore()

	st := s.d.overlord.State()
	st.Lock()
	addNotice(c, st, 0, state.ChangeUpdateNotice, "123", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, st, 1000, state.CustomNotice, "a.b/x", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, st, 123, state.CustomNotice, "a.b/y", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, st, 0, state.WarningNotice, "danger", &state.AddNoticeOptions{
		Visibility: state.PublicNotice,
	})
	addNotice(c, st, 1000, state.ChangeUpdateNotice, "456", &state.AddNoticeOptions{
		Visibility: state.PublicNotice,
	})
	addNotice(c, st, 123, state.ChangeUpdateNotice, "789", &state.AddNoticeOptions{
		Visibility: state.PublicNotice,
	})
	st.Unlock()

	noticesCmd := apiCmd("/v1/notices")

	// Test that a connection with unknown UID only receives public notices.
	req, err := http.NewRequest("GET", "/v1/notices", nil)
	c.Assert(err, IsNil)
	req.RemoteAddr = "pid=100;uid=;socket=;"
	rsp, ok := noticesCmd.GET(noticesCmd, req, nil).(*resp)
	c.Assert(ok, Equals, true)

	c.Check(rsp.Type, Equals, ResponseTypeSync)
	c.Check(rsp.Status, Equals, http.StatusOK)
	notices, ok := rsp.Result.([]*state.Notice)
	c.Assert(ok, Equals, true)
	c.Assert(notices, HasLen, 3)
	n := noticeToMap(c, notices[0])
	c.Assert(n["user-id"], Equals, 0.0)
	c.Assert(n["visibility"], Equals, "public")
	n = noticeToMap(c, notices[1])
	c.Assert(n["user-id"], Equals, 1000.0)
	c.Assert(n["visibility"], Equals, "public")
	n = noticeToMap(c, notices[2])
	c.Assert(n["user-id"], Equals, 123.0)
	c.Assert(n["visibility"], Equals, "public")
}

func (s *apiSuite) TestNoticesFilterMultipleVisibilities(c *C) {
	s.daemon(c)
	restore := mockSysGetuid(0)
	defer restore()

	st := s.d.overlord.State()
	st.Lock()
	addNotice(c, st, 1000, state.ChangeUpdateNotice, "123", nil)
	addNotice(c, st, 1000, state.CustomNotice, "a.b/x", &state.AddNoticeOptions{
		Visibility: state.PublicNotice,
	})
	addNotice(c, st, 1000, state.WarningNotice, "danger", &state.AddNoticeOptions{
		Visibility: state.PrivateNotice,
	})
	st.Unlock()

	for _, testCase := range []struct {
		visibilitiesFilter   string
		expectedVisibilities []string
	}{
		{
			"visibilities=private",
			[]string{"private", "private"},
		},
		{
			"visibilities=public",
			[]string{"public"},
		},
		{
			"visibilities=private&visibilities=public",
			[]string{"private", "public", "private"},
		},
	} {
		req, err := http.NewRequest("GET", fmt.Sprintf("/v1/notices?%s", testCase.visibilitiesFilter), nil)
		req.RemoteAddr = "pid=100;uid=1000;socket=;"
		c.Assert(err, IsNil)
		noticesCmd := apiCmd("/v1/notices")
		rsp, ok := noticesCmd.GET(noticesCmd, req, nil).(*resp)
		c.Assert(ok, Equals, true)

		c.Check(rsp.Type, Equals, ResponseTypeSync)
		c.Check(rsp.Status, Equals, http.StatusOK)
		notices, ok := rsp.Result.([]*state.Notice)
		c.Assert(ok, Equals, true)
		c.Assert(notices, HasLen, len(testCase.expectedVisibilities))
		for i, visibility := range testCase.expectedVisibilities {
			n := noticeToMap(c, notices[i])
			c.Assert(n["visibility"], Equals, visibility)
		}
	}
}

func (s *apiSuite) TestNoticesWait(c *C) {
	s.daemon(c)
	restore := mockSysGetuid(0)
	defer restore()

	st := s.d.overlord.State()
	go func() {
		time.Sleep(10 * time.Millisecond)
		st.Lock()
		addNotice(c, st, 1000, state.CustomNotice, "a.b/1", nil)
		st.Unlock()
	}()

	req, err := http.NewRequest("GET", "/v1/notices?timeout=1s", nil)
	req.RemoteAddr = "pid=100;uid=1000;socket=;"
	c.Assert(err, IsNil)
	noticesCmd := apiCmd("/v1/notices")
	rsp, ok := noticesCmd.GET(noticesCmd, req, nil).(*resp)
	c.Assert(ok, Equals, true)

	c.Check(rsp.Type, Equals, ResponseTypeSync)
	c.Check(rsp.Status, Equals, http.StatusOK)
	notices, ok := rsp.Result.([]*state.Notice)
	c.Assert(ok, Equals, true)
	c.Assert(notices, HasLen, 1)
	n := noticeToMap(c, notices[0])
	c.Check(n["user-id"], Equals, 1000.0)
	c.Check(n["type"], Equals, "custom")
	c.Check(n["key"], Equals, "a.b/1")
	c.Check(n["visibility"], Equals, "private")
}

func (s *apiSuite) TestNoticesTimeout(c *C) {
	s.daemon(c)
	restore := mockSysGetuid(0)
	defer restore()

	req, err := http.NewRequest("GET", "/v1/notices?timeout=1ms", nil)
	c.Assert(err, IsNil)
	noticesCmd := apiCmd("/v1/notices")
	rsp, ok := noticesCmd.GET(noticesCmd, req, nil).(*resp)
	c.Assert(ok, Equals, true)

	c.Check(rsp.Type, Equals, ResponseTypeSync)
	c.Check(rsp.Status, Equals, http.StatusOK)
	notices, ok := rsp.Result.([]*state.Notice)
	c.Assert(ok, Equals, true)
	c.Assert(notices, HasLen, 0)
}

func (s *apiSuite) TestNoticesRequestCancelled(c *C) {
	s.daemon(c)
	restore := mockSysGetuid(0)
	defer restore()

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
	result, ok := rsp.Result.(*errorResult)
	c.Assert(ok, Equals, true)
	c.Check(result.Message, Matches, "request canceled")

	elapsed := time.Since(start)
	c.Check(elapsed > 10*time.Millisecond, Equals, true)
	c.Check(elapsed < time.Second, Equals, true)
}

func (s *apiSuite) TestNoticesInvalidAfter(c *C) {
	restore := mockSysGetuid(0)
	defer restore()
	s.testNoticesBadRequest(c, "after=foo", `invalid "after" timestamp.*`)
}

func (s *apiSuite) TestNoticesInvalidTimeout(c *C) {
	restore := mockSysGetuid(0)
	defer restore()
	s.testNoticesBadRequest(c, "timeout=foo", "invalid timeout.*")
}

func (s *apiSuite) testNoticesBadRequest(c *C, query, errorMatch string) {
	s.daemon(c)

	req, err := http.NewRequest("GET", "/v1/notices?"+query, nil)
	req.RemoteAddr = "pid=100;uid=1000;socket=;"
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
	restore := mockSysGetuid(0)
	defer restore()

	start := time.Now()
	body := []byte(`{
		"action": "add",
		"type": "custom",
		"key": "a.b/1",
		"visibility": "public",
		"repeat-after": "1h",
		"data": {"k": "v"}
	}`)
	req, err := http.NewRequest("POST", "/v1/notices", bytes.NewReader(body))
	req.RemoteAddr = "pid=100;uid=0;socket=;"
	c.Assert(err, IsNil)
	noticesCmd := apiCmd("/v1/notices")
	rsp, ok := noticesCmd.POST(noticesCmd, req, nil).(*resp)
	c.Assert(ok, Equals, true)

	c.Check(rsp.Type, Equals, ResponseTypeSync)
	c.Check(rsp.Status, Equals, http.StatusOK)
	resultBytes, err := json.Marshal(rsp.Result)
	c.Assert(err, IsNil)

	st := s.d.overlord.State()
	st.Lock()
	notices := st.Notices(nil)
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
		"user-id":      0.0,
		"type":         "custom",
		"key":          "a.b/1",
		"visibility":   "public",
		"occurrences":  1.0,
		"last-data":    map[string]any{"k": "v"},
		"expire-after": "168h0m0s",
		"repeat-after": "1h0m0s",
	})
}

func (s *apiSuite) TestAddNoticeMinimal(c *C) {
	s.daemon(c)
	restore := mockSysGetuid(0)
	defer restore()

	body := []byte(`{
		"action": "add",
		"type": "custom",
		"key": "a.b/1"
	}`)
	req, err := http.NewRequest("POST", "/v1/notices", bytes.NewReader(body))
	req.RemoteAddr = "pid=100;uid=1000;socket=;"
	c.Assert(err, IsNil)
	noticesCmd := apiCmd("/v1/notices")
	rsp, ok := noticesCmd.POST(noticesCmd, req, nil).(*resp)
	c.Assert(ok, Equals, true)

	c.Check(rsp.Type, Equals, ResponseTypeSync)
	c.Check(rsp.Status, Equals, http.StatusOK)
	resultBytes, err := json.Marshal(rsp.Result)
	c.Assert(err, IsNil)

	st := s.d.overlord.State()
	st.Lock()
	notices := st.Notices(nil)
	st.Unlock()
	c.Assert(notices, HasLen, 1)
	n := noticeToMap(c, notices[0])
	noticeId, ok := n["id"].(string)
	c.Assert(ok, Equals, true)
	c.Assert(string(resultBytes), Equals, `{"id":"`+noticeId+`"}`)

	delete(n, "first-occurred")
	delete(n, "last-occurred")
	delete(n, "last-repeated")
	c.Assert(n, DeepEquals, map[string]any{
		"id":           noticeId,
		"user-id":      1000.0,
		"type":         "custom",
		"key":          "a.b/1",
		"visibility":   "private",
		"occurrences":  1.0,
		"expire-after": "168h0m0s",
	})
}

func (s *apiSuite) TestAddNoticeMismatchedVisibility(c *C) {
	s.daemon(c)
	restore := mockSysGetuid(0)
	defer restore()

	requestMap := map[string]any{
		"action": "add",
		"type": "custom",
		"key": "a.b/1",
		"visibility": "public",
	}

	// Test that a notice can be added as public, then private, then public
	// without returning an error.
	for _, visibility := range []string{"public", "private", "public"} {
		requestMap["visibility"] = visibility
		requestBody, err := json.Marshal(requestMap)
		c.Assert(err, IsNil)
		req, err := http.NewRequest("POST", "/v1/notices", bytes.NewReader(requestBody))
		req.RemoteAddr = "pid=100;uid=0;socket=;"
		c.Assert(err, IsNil)
		noticesCmd := apiCmd("/v1/notices")
		rsp, ok := noticesCmd.POST(noticesCmd, req, nil).(*resp)
		c.Assert(ok, Equals, true)
		c.Check(rsp.Type, Equals, ResponseTypeSync)
		c.Check(rsp.Status, Equals, http.StatusOK)
		resultBytes, err := json.Marshal(rsp.Result)
		c.Assert(err, IsNil)

		st := s.d.overlord.State()
		st.Lock()
		notices := st.Notices(nil)
		st.Unlock()
		c.Assert(notices, HasLen, 1)
		n := noticeToMap(c, notices[0])
		noticeId, ok := n["id"].(string)
		c.Assert(ok, Equals, true)
		c.Assert(string(resultBytes), Equals, `{"id":"`+noticeId+`"}`)
		c.Assert(n["visibility"], Equals, visibility)
	}
}

func (s *apiSuite) TestAddNoticeInvalidRequestUid(c *C) {
	s.daemon(c)
	restore := mockSysGetuid(0)
	defer restore()

	body := []byte(`{
		"action": "add",
		"type": "custom",
		"key": "a.b/1"
	}`)
	req, err := http.NewRequest("POST", "/v1/notices", bytes.NewReader(body))
	req.RemoteAddr = "pid=100;uid=;socket=;"
	c.Assert(err, IsNil)
	noticesCmd := apiCmd("/v1/notices")
	rsp, ok := noticesCmd.POST(noticesCmd, req, nil).(*resp)
	c.Assert(ok, Equals, true)

	c.Check(rsp.Type, Equals, ResponseTypeError)
	c.Check(rsp.Status, Equals, http.StatusBadRequest)

	result, ok := rsp.Result.(*errorResult)
	c.Assert(ok, Equals, true)
	c.Assert(result.Message, Matches, "cannot determine UID of request.*")
}

func (s *apiSuite) TestAddNoticeInvalidAction(c *C) {
	restore := mockSysGetuid(0)
	defer restore()
	s.testAddNoticeBadRequest(c, `{"action": "bad"}`, "invalid action.*")
}

func (s *apiSuite) TestAddNoticeInvalidType(c *C) {
	restore := mockSysGetuid(0)
	defer restore()
	s.testAddNoticeBadRequest(c, `{"action": "add", "type": "foo"}`, "invalid type.*")
}

func (s *apiSuite) TestAddNoticeInvalidKey(c *C) {
	restore := mockSysGetuid(0)
	defer restore()
	s.testAddNoticeBadRequest(c, `{"action": "add", "type": "custom", "key": "bad"}`,
		"invalid key.*")
}

func (s *apiSuite) TestAddNoticeKeyTooLong(c *C) {
	restore := mockSysGetuid(0)
	defer restore()
	request, err := json.Marshal(map[string]any{
		"action": "add",
		"type":   "custom",
		"key":    "a.b/" + strings.Repeat("x", 257-4),
	})
	c.Assert(err, IsNil)
	s.testAddNoticeBadRequest(c, string(request), "key must be 256 bytes or less")
}

func (s *apiSuite) TestAddNoticeInvalidVisibility(c *C) {
	// Only root (or admin if not run as root) may create public notices
	restore := mockSysGetuid(0)
	defer restore()
	request, err := json.Marshal(map[string]any{
		"action":     "add",
		"type":       "custom",
		"key":        "a.b/c",
		"visibility": "public",
	})
	c.Assert(err, IsNil)
	s.testAddNoticeBadRequest(c, string(request), "invalid visibility.*")

	// Now try with connection as admin
	restore2 := mockSysGetuid(123)
	defer restore2()
	body := []byte(`{
		"action":     "add",
		"type":       "custom",
		"key":        "a.b/c",
		"visibility": "public"
	}`)
	req, err := http.NewRequest("POST", "/v1/notices", bytes.NewReader(body))
	req.RemoteAddr = "pid=100;uid=123;socket=;"
	c.Assert(err, IsNil)
	noticesCmd := apiCmd("/v1/notices")
	rsp, ok := noticesCmd.POST(noticesCmd, req, nil).(*resp)
	c.Assert(ok, Equals, true)
	c.Check(rsp.Type, Equals, ResponseTypeSync)
	c.Check(rsp.Status, Equals, http.StatusOK)
}

func (s *apiSuite) TestAddNoticeDataTooLarge(c *C) {
	restore := mockSysGetuid(0)
	defer restore()
	request, err := json.Marshal(map[string]any{
		"action": "add",
		"type":   "custom",
		"key":    "a.b/c",
		"data": map[string]string{
			"a": strings.Repeat("x", 2047),
			"b": strings.Repeat("y", 2048),
		},
	})
	c.Assert(err, IsNil)
	s.testAddNoticeBadRequest(c, string(request), "total size of data must be 4096 bytes or less")
}

func (s *apiSuite) TestAddNoticeInvalidRepeatAfter(c *C) {
	restore := mockSysGetuid(0)
	defer restore()
	s.testAddNoticeBadRequest(c, `{"action": "add", "type": "custom", "key": "a.b/1", "repeat-after": "bad"}`,
		"invalid repeat-after.*")
}

func (s *apiSuite) testAddNoticeBadRequest(c *C, body, errorMatch string) {
	s.daemon(c)

	req, err := http.NewRequest("POST", "/v1/notices", strings.NewReader(body))
	req.RemoteAddr = "pid=100;uid=1000;socket=;"
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
	restore := mockSysGetuid(0)
	defer restore()

	st := s.d.overlord.State()
	st.Lock()
	addNotice(c, st, 1000, state.CustomNotice, "a.b/1", nil)
	noticeId, err := st.AddNotice(1000, state.CustomNotice, "a.b/2", nil)
	c.Assert(err, IsNil)
	addNotice(c, st, 1000, state.CustomNotice, "a.b/3", nil)
	st.Unlock()

	req, err := http.NewRequest("GET", "/v1/notices/"+noticeId, nil)
	req.RemoteAddr = "pid=100;uid=1000;socket=;"
	c.Assert(err, IsNil)
	noticesCmd := apiCmd("/v1/notices/{id}")
	s.vars = map[string]string{"id": noticeId}
	rsp, ok := noticesCmd.GET(noticesCmd, req, nil).(*resp)
	c.Assert(ok, Equals, true)

	c.Check(rsp.Type, Equals, ResponseTypeSync)
	c.Check(rsp.Status, Equals, http.StatusOK)
	notice, ok := rsp.Result.(*state.Notice)
	c.Assert(ok, Equals, true)
	n := noticeToMap(c, notice)
	c.Check(n["type"], Equals, "custom")
	c.Check(n["key"], Equals, "a.b/2")
}

func (s *apiSuite) TestNoticeNotFound(c *C) {
	s.daemon(c)
	restore := mockSysGetuid(0)
	defer restore()

	req, err := http.NewRequest("GET", "/v1/notices/1234", nil)
	req.RemoteAddr = "pid=100;uid=1000;socket=;"
	c.Assert(err, IsNil)
	noticesCmd := apiCmd("/v1/notices/{id}")
	s.vars = map[string]string{"id": "1234"}
	rsp, ok := noticesCmd.GET(noticesCmd, req, nil).(*resp)
	c.Assert(ok, Equals, true)

	c.Check(rsp.Type, Equals, ResponseTypeError)
	c.Check(rsp.Status, Equals, 404)
}

func (s *apiSuite) TestNoticeNotAllowed(c *C) {
	s.daemon(c)
	restore := mockSysGetuid(0)
	defer restore()

	st := s.d.overlord.State()
	st.Lock()
	noticeId, err := st.AddNotice(1000, state.CustomNotice, "a.b/1", nil)
	c.Assert(err, IsNil)
	st.Unlock()

	req, err := http.NewRequest("GET", "/v1/notices/"+noticeId, nil)
	c.Assert(err, IsNil)
	req.RemoteAddr = "pid=100;uid=1001;socket=;"
	noticesCmd := apiCmd("/v1/notices/{id}")
	s.vars = map[string]string{"id": noticeId}
	rsp, ok := noticesCmd.GET(noticesCmd, req, nil).(*resp)
	c.Assert(ok, Equals, true)

	c.Check(rsp.Type, Equals, ResponseTypeError)
	c.Check(rsp.Status, Equals, http.StatusForbidden)
	notice, ok := rsp.Result.(*state.Notice)
	c.Assert(ok, Equals, false)
	c.Assert(notice, IsNil)
}

func noticeToMap(c *C, notice *state.Notice) map[string]any {
	buf, err := json.Marshal(notice)
	c.Assert(err, IsNil)
	var n map[string]any
	err = json.Unmarshal(buf, &n)
	c.Assert(err, IsNil)
	return n
}

func addNotice(c *C, st *state.State, userID uint32, noticeType state.NoticeType, key string, options *state.AddNoticeOptions) {
	_, err := st.AddNotice(userID, noticeType, key, options)
	c.Assert(err, IsNil)
}
