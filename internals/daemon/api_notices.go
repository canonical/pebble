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
	"fmt"
	"net/http"
	"regexp"
	"strconv"

	"github.com/canonical/x-go/strutil"

	"github.com/canonical/pebble/internals/overlord/state"
)

// Ensure custom keys are in the form "domain.com/key" (but somewhat more restrictive).
var customKeyRegexp = regexp.MustCompile(
	`^[a-z0-9]+(-[a-z0-9]+)*(\.[a-z0-9]+(-[a-z0-9]+)*)+(/[a-z0-9]+(-[a-z0-9]+)*)+$`)

const (
	maxNoticeKeyLength = 256
	maxNoticeDataSize  = 4 * 1024
)

type addedNotice struct {
	ID string `json:"id"`
}

var (
	errUserIDFilterNoNotices = errors.New("no notices possible with user IDs filter for given request")
)

func v1GetNotices(c *Command, r *http.Request, _ *UserState) Response {
	query := r.URL.Query()

	reqUid := uidFromRequest(r)

	userIDs, err := sanitizeUserIDsFilter(reqUid, query["user-ids"])
	if err == errUserIDFilterNoNotices {
		// User IDs filter precluded any possible notices, so return empty list.
		return SyncResponse([]*state.Notice{})
	} else if err != nil {
		return statusBadRequest(`invalid "user-ids" filter: %v`, err)
	}

	typeStrs := strutil.MultiCommaSeparatedList(query["types"])
	types := make([]state.NoticeType, 0, len(typeStrs))
	for _, typeStr := range typeStrs {
		noticeType := state.NoticeType(typeStr)
		if !noticeType.Valid() {
			// Ignore invalid notice types (so requests from newer clients
			// with unknown types succeed).
			continue
		}
		types = append(types, noticeType)
	}

	keys := strutil.MultiCommaSeparatedList(query["keys"])

	after, err := parseOptionalTime(query.Get("after"))
	if err != nil {
		return statusBadRequest(`invalid "after" timestamp: %v`, err)
	}

	filter := &state.NoticeFilter{
		Types:   types,
		Keys:    keys,
		UserIDs: userIDs,
		After:   after,
	}
	var notices []*state.Notice

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	timeout, err := parseOptionalDuration(query.Get("timeout"))
	if err != nil {
		return statusBadRequest("invalid timeout: %v", err)
	}
	if timeout != 0 {
		// Wait up to timeout for notices matching given filter to occur
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()

		notices, err = st.WaitNotices(ctx, filter)
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
		notices = st.Notices(filter)
	}

	if notices == nil {
		notices = []*state.Notice{} // avoid null result
	}
	return SyncResponse(notices)
}

// Get the UID of the request. If the UID is not known, return -1, indicating
// that the connection may only receive notices intended for any recipient.
func uidFromRequest(r *http.Request) int {
	_, uid, _, err := ucrednetGet(r.RemoteAddr)
	if err != nil {
		return -1
	}
	return int(uid)
}

// Construct the user IDs filter which will be passed into the notices state.
// Importantly, ensure that non-root users cannot filter on user IDs other than
// their own and -1.
func sanitizeUserIDsFilter(reqUid int, queryUserIDs []string) ([]int, error) {
	userIDStrs := strutil.MultiCommaSeparatedList(queryUserIDs)
	userIDs := make([]int, 0, len(userIDStrs))
	for _, userIDStr := range userIDStrs {
		uid, err := strconv.ParseInt(userIDStr, 10, 64)
		if err != nil {
			return nil, err
		}
		userID := int(uid)
		if err := state.ValidateUserID(&userID); err != nil {
			return nil, fmt.Errorf(`invalid user ID "%d": %v`, userID, err)
		}
		userIDs = append(userIDs, userID)
	}
	if reqUid == 0 {
		// Request from root, allow filtering by user ID without restriction.
		return userIDs, nil
	}
	// Only allow non-root users to see notices with matching UID or UID of -1.
	if len(userIDs) == 0 {
		return []int{-1, reqUid}, nil
	}
	// If a non-root request has user IDs filter, only permit the intersection
	// of {-1, reqUid} and the requested user IDs.
	sanitized := make([]int, 0, 2)
	for _, uid := range userIDs {
		if uid == reqUid || uid == -1 {
			sanitized = append(sanitized, uid)
		}
	}
	if len(sanitized) == 0 {
		// Requested notices to only user IDs for which the UID
		// associated with the request does not have access.
		return nil, errUserIDFilterNoNotices
	}
	return sanitized, nil
}

func v1PostNotices(c *Command, r *http.Request, _ *UserState) Response {
	var payload struct {
		Action      string          `json:"action"`
		Type        string          `json:"type"`
		Key         string          `json:"key"`
		UserID      *int            `json:"user-id"`
		RepeatAfter string          `json:"repeat-after"`
		DataJSON    json.RawMessage `json:"data"`
	}
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&payload); err != nil {
		return statusBadRequest("cannot decode request body: %v", err)
	}

	if payload.Action != "add" {
		return statusBadRequest("invalid action %q", payload.Action)
	}
	if err := state.ValidateUserID(payload.UserID); err != nil {
		return statusBadRequest(`invalid user ID %d: %v`, *payload.UserID, err)
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

	repeatAfter, err := parseOptionalDuration(payload.RepeatAfter)
	if err != nil {
		return statusBadRequest("invalid repeat-after: %v", err)
	}

	if len(payload.DataJSON) > maxNoticeDataSize {
		return statusBadRequest("total size of data must be %d bytes or less", maxNoticeDataSize)
	}
	var data map[string]string
	if len(payload.DataJSON) > 0 {
		err = json.Unmarshal(payload.DataJSON, &data)
		if err != nil {
			return statusBadRequest("cannot decode notice data: %v", err)
		}
	}

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	noticeId, err := st.AddNotice(state.CustomNotice, payload.Key, &state.AddNoticeOptions{
		UserID:      payload.UserID,
		Data:        data,
		RepeatAfter: repeatAfter,
	})
	if err != nil {
		return statusInternalError("%v", err)
	}

	return SyncResponse(addedNotice{ID: noticeId})
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
	reqUid := uidFromRequest(r)
	noticeUid := notice.UserID()
	viewable := reqUid == 0 || reqUid == noticeUid || noticeUid == -1
	if !viewable {
		// Requests from non-root users may only receive notices with matching
		// UID or UID of -1.
		return statusForbidden("not allowed to access notice with id %q", noticeID)
	}
	return SyncResponse(notice)
}
