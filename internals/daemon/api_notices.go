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
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"time"

	"github.com/canonical/x-go/strutil"

	"github.com/canonical/pebble/internals/overlord/state"
)

// A very loose regex to ensure custom keys are in the form "domain.com/key"
var customKeyRegexp = regexp.MustCompile(`^([a-z0-9-]+\.)+[a-z0-9-]+/[A-Za-z0-9./-]+$`)

const (
	maxNoticeKeyLength = 256
	maxNoticeDataSize  = 4 * 1024
)

func v1GetNotices(c *Command, r *http.Request, _ *UserState) Response {
	query := r.URL.Query()

	typeStrs := strutil.MultiCommaSeparatedList(query["types"])
	types := make([]state.NoticeType, 0, len(typeStrs))
	for _, typeStr := range typeStrs {
		noticeType := state.NoticeTypeFromString(typeStr)
		if noticeType == "" {
			return statusBadRequest("invalid notice type %q", typeStr)
		}
		types = append(types, noticeType)
	}

	keys := strutil.MultiCommaSeparatedList(query["keys"])

	afterStr := query.Get("after")
	var after time.Time
	if afterStr != "" {
		var err error
		after, err = time.Parse(time.RFC3339, afterStr)
		if err != nil {
			return statusBadRequest("invalid after timestamp %q: %v", afterStr, err)
		}
	}

	filters := state.NoticeFilters{
		Types: types,
		Keys:  keys,
		After: after,
	}
	var notices []*state.Notice

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	timeoutStr := query.Get("timeout")
	if timeoutStr != "" {
		// Wait up to timeout for notices matching given filters to occur
		timeout, err := time.ParseDuration(timeoutStr)
		if err != nil {
			return statusBadRequest("invalid timeout %q: %v", timeoutStr, err)
		}

		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()

		notices, err = st.WaitNotices(ctx, filters)
		if errors.Is(err, context.Canceled) {
			return statusBadRequest("request canceled")
		}
		// DeadlineExceeded will occur if timeout elapses; in that case return
		// an empty list of notices, not an error.
		if err != nil && !errors.Is(err, context.DeadlineExceeded) {
			return statusInternalError("cannot wait for notices: %s", err)
		}
	} else {
		// No timeout given, fetch currently-available notices
		notices = st.Notices(filters)
	}

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
	if payload.Type != "custom" {
		return statusBadRequest(`invalid type %q (can only add "custom" notices)`, payload.Type)
	}
	if !customKeyRegexp.MatchString(payload.Key) {
		return statusBadRequest(`invalid key %q (must be in "domain.com/key" format)`, payload.Key)
	}
	if len(payload.Key) > maxNoticeKeyLength {
		return statusBadRequest("key must be %d bytes or less", maxNoticeKeyLength)
	}

	var repeatAfter time.Duration
	if payload.RepeatAfter != "" {
		var err error
		repeatAfter, err = time.ParseDuration(payload.RepeatAfter)
		if err != nil {
			return statusBadRequest("invalid repeat-after duration %q: %v", payload.RepeatAfter, err)
		}
	}

	dataSize := 0
	for k, v := range payload.Data {
		dataSize += len(k) + len(v)
	}
	if dataSize > maxNoticeDataSize {
		return statusBadRequest("total size of data (keys and values) must be %d bytes or less", maxNoticeDataSize)
	}

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	noticeId := st.AddNotice(state.NoticeCustom, payload.Key, payload.Data, repeatAfter)

	result := struct {
		ID string `json:"id"`
	}{
		ID: noticeId,
	}
	return SyncResponse(result)
}

func v1GetNotice(c *Command, r *http.Request, _ *UserState) Response {
	noticeID := muxVars(r)["id"]
	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()
	notice := st.Notice(noticeID)
	if notice == nil {
		return statusNotFound("cannot find notice with id %q", noticeID)
	}
	return SyncResponse(notice)
}
