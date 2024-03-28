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

package client_test

import (
	"context"
	"encoding/json"
	"io"
	"net/url"
	"time"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/client"
)

func (cs *clientSuite) TestNotice(c *C) {
	cs.rsp = `{"type": "sync", "result": {
		"id": "123",
		"user-id": 1000,
		"type": "custom",
		"key": "a.b/c",
		"first-occurred": "2023-09-05T15:43:00.123Z",
		"last-occurred": "2023-09-05T17:43:00.567Z",
		"last-repeated": "2023-09-05T16:43:00Z",
		"occurrences": 7,
		"last-data": {"k": "v"},
		"repeat-after": "1h",
		"expire-after": "168h"
	}}`
	notice, err := cs.cli.Notice("123")
	c.Assert(err, IsNil)
	uid := uint32(1000)
	c.Assert(notice, DeepEquals, &client.Notice{
		ID:            "123",
		UserID:        &uid,
		Type:          client.CustomNotice,
		Key:           "a.b/c",
		FirstOccurred: time.Date(2023, 9, 5, 15, 43, 0, 123_000_000, time.UTC),
		LastOccurred:  time.Date(2023, 9, 5, 17, 43, 0, 567_000_000, time.UTC),
		LastRepeated:  time.Date(2023, 9, 5, 16, 43, 0, 0, time.UTC),
		Occurrences:   7,
		LastData:      map[string]string{"k": "v"},
		RepeatAfter:   time.Hour,
		ExpireAfter:   7 * 24 * time.Hour,
	})
}

func (cs *clientSuite) TestChangeUpdateNotice(c *C) {
	cs.rsp = `{"type": "sync", "result": {
		"id": "456",
		"user-id": 1001,
		"type": "change-update",
		"key": "42",
		"first-occurred": "2024-09-05T15:43:00.123Z",
		"last-occurred": "2024-09-05T17:43:00.567Z",
		"last-repeated": "2024-09-05T16:43:00Z",
		"occurrences": 3
	}}`
	notice, err := cs.cli.Notice("123")
	c.Assert(err, IsNil)
	uid := uint32(1001)
	c.Assert(notice, DeepEquals, &client.Notice{
		ID:            "456",
		UserID:        &uid,
		Type:          client.ChangeUpdateNotice,
		Key:           "42",
		FirstOccurred: time.Date(2024, 9, 5, 15, 43, 0, 123_000_000, time.UTC),
		LastOccurred:  time.Date(2024, 9, 5, 17, 43, 0, 567_000_000, time.UTC),
		LastRepeated:  time.Date(2024, 9, 5, 16, 43, 0, 0, time.UTC),
		Occurrences:   3,
	})
}

func (cs *clientSuite) TestNoticeInvalidID(c *C) {
	_, err := cs.cli.Notice("<boo>")
	c.Assert(err, ErrorMatches, "invalid notice ID.*")
}

func (cs *clientSuite) TestNotices(c *C) {
	cs.rsp = `{"type": "sync", "result": [{
		"id":   "1",
		"user-id": 1000,
		"type": "custom",
		"key": "a.b/c",
		"first-occurred": "2023-09-05T15:43:00.123Z",
		"last-occurred": "2023-09-05T17:43:00.567Z",
		"last-repeated": "2023-09-05T16:43:00Z",
		"occurrences": 7,
		"last-data": {"k": "v"},
		"repeat-after": "1h",
		"expire-after": "168h"
	}, {
		"id":   "2",
		"user-id": null,
		"type": "warning",
		"key": "be careful!",
		"first-occurred": "2023-09-06T15:43:00.123Z",
		"last-occurred": "2023-09-06T17:43:00.567Z",
		"last-repeated": "2023-09-06T16:43:00Z",
		"occurrences": 1
	}]}`
	notices, err := cs.cli.Notices(&client.NoticesOptions{})
	c.Assert(err, IsNil)
	c.Assert(cs.req.URL.Path, Equals, "/v1/notices")
	c.Assert(cs.req.URL.Query(), DeepEquals, url.Values{})
	uid := uint32(1000)
	c.Assert(notices, DeepEquals, []*client.Notice{{
		ID:            "1",
		UserID:        &uid,
		Type:          "custom",
		Key:           "a.b/c",
		FirstOccurred: time.Date(2023, 9, 5, 15, 43, 0, 123_000_000, time.UTC),
		LastOccurred:  time.Date(2023, 9, 5, 17, 43, 0, 567_000_000, time.UTC),
		LastRepeated:  time.Date(2023, 9, 5, 16, 43, 0, 0, time.UTC),
		Occurrences:   7,
		LastData:      map[string]string{"k": "v"},
		RepeatAfter:   time.Hour,
		ExpireAfter:   7 * 24 * time.Hour,
	}, {
		ID:            "2",
		UserID:        nil,
		Type:          "warning",
		Key:           "be careful!",
		FirstOccurred: time.Date(2023, 9, 6, 15, 43, 0, 123_000_000, time.UTC),
		LastOccurred:  time.Date(2023, 9, 6, 17, 43, 0, 567_000_000, time.UTC),
		LastRepeated:  time.Date(2023, 9, 6, 16, 43, 0, 0, time.UTC),
		Occurrences:   1,
	}})
}

func (cs *clientSuite) TestNoticesFilters(c *C) {
	cs.rsp = `{"type": "sync", "result": []}`
	uid := uint32(1000)
	notices, err := cs.cli.Notices(&client.NoticesOptions{
		Users:  client.NoticesUsersAll,
		UserID: &uid,
		Types:  []client.NoticeType{client.CustomNotice},
		Keys:   []string{"foo.com/bar", "example.com/x"},
		After:  time.Date(2023, 9, 5, 16, 43, 32, 123_456_789, time.UTC),
	})
	c.Assert(err, IsNil)
	c.Assert(cs.req.URL.Path, Equals, "/v1/notices")
	c.Assert(cs.req.URL.Query(), DeepEquals, url.Values{
		"users":   {"all"},
		"user-id": {"1000"},
		"types":   {"custom"},
		"keys":    {"foo.com/bar", "example.com/x"},
		"after":   {"2023-09-05T16:43:32.123456789Z"},
	})
	c.Assert(notices, DeepEquals, []*client.Notice{})
}

func (cs *clientSuite) TestNotify(c *C) {
	cs.rsp = `{"type": "sync", "result": {"id": "7"}}`
	noticeID, err := cs.cli.Notify(&client.NotifyOptions{
		Type:        client.CustomNotice,
		Key:         "foo.com/bar",
		RepeatAfter: time.Hour,
		Data:        map[string]string{"k": "9"},
	})
	c.Assert(err, IsNil)
	c.Check(noticeID, Equals, "7")
	c.Assert(cs.req.URL.Path, Equals, "/v1/notices")

	body, err := io.ReadAll(cs.req.Body)
	c.Assert(err, IsNil)
	var m map[string]any
	err = json.Unmarshal(body, &m)
	c.Assert(err, IsNil)
	c.Assert(m, DeepEquals, map[string]any{
		"action":       "add",
		"type":         "custom",
		"key":          "foo.com/bar",
		"data":         map[string]any{"k": "9"},
		"repeat-after": "1h0m0s",
	})
}

func (cs *clientSuite) TestNotifyMinimal(c *C) {
	cs.rsp = `{"type": "sync", "result": {"id": "1"}}`
	noticeID, err := cs.cli.Notify(&client.NotifyOptions{
		Type: client.CustomNotice,
		Key:  "a.b/c",
	})
	c.Assert(err, IsNil)
	c.Check(noticeID, Equals, "1")
	c.Assert(cs.req.URL.Path, Equals, "/v1/notices")

	body, err := io.ReadAll(cs.req.Body)
	c.Assert(err, IsNil)
	var m map[string]any
	err = json.Unmarshal(body, &m)
	c.Assert(err, IsNil)
	c.Assert(m, DeepEquals, map[string]any{
		"action": "add",
		"type":   "custom",
		"key":    "a.b/c",
	})
}

func (cs *clientSuite) TestWaitNotices(c *C) {
	cs.rsp = `{"type": "sync", "result": [{
		"id":   "1",
		"user-id": 1000,
		"type": "warning",
		"key": "be careful!",
		"first-occurred": "2023-09-06T15:43:00.123Z",
		"last-occurred": "2023-09-06T17:43:00.567Z",
		"last-repeated": "2023-09-06T16:43:00Z",
		"occurrences": 1
	}]}`
	notices, err := cs.cli.WaitNotices(context.Background(), 10*time.Second, nil)
	c.Assert(err, IsNil)
	c.Assert(cs.req.URL.Path, Equals, "/v1/notices")
	c.Assert(cs.req.URL.Query(), DeepEquals, url.Values{
		"timeout": {"10s"},
	})
	uid := uint32(1000)
	c.Assert(notices, DeepEquals, []*client.Notice{{
		ID:            "1",
		UserID:        &uid,
		Type:          "warning",
		Key:           "be careful!",
		FirstOccurred: time.Date(2023, 9, 6, 15, 43, 0, 123_000_000, time.UTC),
		LastOccurred:  time.Date(2023, 9, 6, 17, 43, 0, 567_000_000, time.UTC),
		LastRepeated:  time.Date(2023, 9, 6, 16, 43, 0, 0, time.UTC),
		Occurrences:   1,
	}})
}

func (cs *clientSuite) TestWaitNoticesTimeout(c *C) {
	cs.rsp = `{"type": "sync", "result": []}`
	notices, err := cs.cli.WaitNotices(context.Background(), time.Second, nil)
	c.Assert(err, IsNil)
	c.Assert(notices, HasLen, 0)
}
