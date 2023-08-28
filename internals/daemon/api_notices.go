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
	"encoding/json"
	"net/http"
	"regexp"
	"time"

	"github.com/canonical/pebble/internals/overlord/state"
)

const maxKeyLength = 255

// A very loose regex to ensure client keys are in the form "domain.com/key"
var clientKeyRegexp = regexp.MustCompile(`([a-z0-9-_]+\.)+[a-z0-9-_]+/.+`)

func v1GetNotices(c *Command, r *http.Request, _ *UserState) Response {
	query := r.URL.Query()

	typeStr := query.Get("type")
	noticeType := state.NoticeTypeFromString(typeStr)
	if typeStr != "" && noticeType == "" {
		return statusBadRequest("invalid notice type %q", typeStr)
	}

	key := query.Get("key")

	afterStr := query.Get("after")
	var after time.Time
	if afterStr != "" {
		var err error
		after, err = time.Parse(time.RFC3339, afterStr)
		if err != nil {
			return statusBadRequest("invalid after timestamp %q: %v", afterStr, err)
		}
	}

	// TODO: hmmm, need a way to communicate/sync with notice changes
	//timeoutStr := query.Get("timeout")
	//var timeout time.Duration
	//if timeoutStr != "" {
	//	var err error
	//	timeout, err = time.ParseDuration(timeoutStr)
	//	if err != nil {
	//		return statusBadRequest("invalid timeout %q: %v", timeoutStr, err)
	//	}
	//}

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	filters := state.NoticeFilters{
		Type:  noticeType,
		Key:   key,
		After: after,
	}
	notices := st.Notices(filters)
	if len(notices) == 0 {
		notices = []*state.Notice{} // avoid null result
	}
	return SyncResponse(notices)
}

func v1PostNotices(c *Command, r *http.Request, _ *UserState) Response {
	var payload struct {
		Action      string            `json:"action"`
		Type        string            `json:"type"`
		Key         string            `json:"key"`
		RepeatAfter string            `json:"repeat-after"`
		Data        map[string]string `json:"data"`
	}
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&payload); err != nil {
		return statusBadRequest("cannot decode request body: %v", err)
	}

	if payload.Action != "add" {
		return statusBadRequest("invalid action %q", payload.Action)
	}
	if payload.Type != "client" {
		return statusBadRequest(`invalid type %q (can only add "client" notices)`, payload.Type)
	}
	if !clientKeyRegexp.MatchString(payload.Key) {
		return statusBadRequest(`invalid key %q (must be in "domain.com/key" format)`, payload.Key)
	}
	if len(payload.Key) > state.MaxNoticeKeyLength {
		return statusBadRequest(`key too long (must be %d bytes or less)`, state.MaxNoticeKeyLength)
	}

	var repeatAfter time.Duration
	if payload.RepeatAfter != "" {
		var err error
		repeatAfter, err = time.ParseDuration(payload.RepeatAfter)
		if err != nil {
			return statusBadRequest("invalid repeat-after duration %q: %v", payload.RepeatAfter, err)
		}
	}

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	st.AddNotice(state.NoticeClient, payload.Key, payload.Data, repeatAfter)

	return SyncResponse(true)
}
