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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"strconv"

	"github.com/canonical/x-go/strutil"

	"github.com/canonical/pebble/internals/overlord/state"
)

// Ensure custom keys are in the form "example.com/path" (but somewhat more restrictive).
var customKeyRegexp = regexp.MustCompile(
	`^[a-z0-9]+(-[a-z0-9]+)*(\.[a-z0-9]+(-[a-z0-9]+)*)+(/[a-z0-9]+(-[a-z0-9]+)*)+$`)

const (
	maxNoticeKeyLength = 256
	maxNoticeDataSize  = 4 * 1024
)

type addedNotice struct {
	ID string `json:"id"`
}

func v1GetNotices(c *Command, r *http.Request, user *UserState) Response {
	// TODO(benhoyt): the design of notices presumes UIDs; if in future when we
	//                support identities that aren't UID based, we'll need to fix this.
	if user == nil || user.UID == nil {
		return Forbidden("cannot determine UID of request, so cannot retrieve notices")
	}

	// By default, return notices with the request UID and public notices.
	userID := user.UID

	query := r.URL.Query()
	if len(query["user-id"]) > 0 {
		if user.Access != state.AdminAccess {
			return Forbidden(`only admins may use the "user-id" filter`)
		}
		var err error
		userID, err = sanitizeUserIDFilter(query["user-id"])
		if err != nil {
			return BadRequest(`invalid "user-id" filter: %v`, err)
		}
	}

	if len(query["users"]) > 0 {
		if user.Access != state.AdminAccess {
			return Forbidden(`only admins may use the "users" filter`)
		}
		if len(query["user-id"]) > 0 {
			return BadRequest(`cannot use both "users" and "user-id" parameters`)
		}
		if query.Get("users") != "all" {
			return BadRequest(`invalid "users" filter: must be "all"`)
		}
		// Clear the userID filter so all notices will be returned.
		userID = nil
	}

	types, err := sanitizeTypesFilter(query["types"])
	if err != nil {
		// Caller did provide a types filter, but they're all invalid notice types.
		// Return no notices, rather than the default of all notices.
		return SyncResponse([]*state.Notice{})
	}

	keys := strutil.MultiCommaSeparatedList(query["keys"])

	after, err := parseOptionalTime(query.Get("after"))
	if err != nil {
		return BadRequest(`invalid "after" timestamp: %v`, err)
	}

	filter := &state.NoticeFilter{
		UserID: userID,
		Types:  types,
		Keys:   keys,
		After:  after,
	}

	timeout, err := parseOptionalDuration(query.Get("timeout"))
	if err != nil {
		return BadRequest("invalid timeout: %v", err)
	}

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	var notices []*state.Notice

	if timeout != 0 {
		// Wait up to timeout for notices matching given filter to occur
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()

		// WaitNotices releases the state lock while waiting.
		notices, err = st.WaitNotices(ctx, filter)
		if errors.Is(err, context.Canceled) {
			return BadRequest("request canceled")
		}
		// DeadlineExceeded will occur if timeout elapses; in that case return
		// an empty list of notices, not an error.
		if err != nil && !errors.Is(err, context.DeadlineExceeded) {
			return InternalError("cannot wait for notices: %s", err)
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

// Construct the user IDs filter which will be passed to state.Notices.
// Must only be called if the query user ID argument is set.
func sanitizeUserIDFilter(queryUserID []string) (*uint32, error) {
	userIDStrs := strutil.MultiCommaSeparatedList(queryUserID)
	if len(userIDStrs) != 1 {
		return nil, fmt.Errorf(`must only include one "user-id"`)
	}
	userIDInt, err := strconv.ParseInt(userIDStrs[0], 10, 64)
	if err != nil {
		return nil, err
	}
	if userIDInt < 0 || userIDInt > math.MaxUint32 {
		return nil, fmt.Errorf("user ID is not a valid uint32: %d", userIDInt)
	}
	userID := uint32(userIDInt)
	return &userID, nil
}

// Construct the types filter which will be passed to state.Notices.
func sanitizeTypesFilter(queryTypes []string) ([]state.NoticeType, error) {
	typeStrs := strutil.MultiCommaSeparatedList(queryTypes)
	alreadySeen := make(map[state.NoticeType]bool, len(typeStrs))
	types := make([]state.NoticeType, 0, len(typeStrs))
	for _, typeStr := range typeStrs {
		noticeType := state.NoticeType(typeStr)
		if !noticeType.Valid() {
			// Ignore invalid notice types (so requests from newer clients
			// with unknown types succeed).
			continue
		}
		if alreadySeen[noticeType] {
			continue
		}
		alreadySeen[noticeType] = true
		types = append(types, noticeType)
	}
	if len(types) == 0 && len(typeStrs) > 0 {
		return nil, errors.New("all requested notice types invalid")
	}
	return types, nil
}

func v1PostNotices(c *Command, r *http.Request, user *UserState) Response {
	if user == nil || user.UID == nil {
		return Forbidden("cannot determine UID of request, so cannot create notice")
	}

	var payload struct {
		Action      string          `json:"action"`
		Type        string          `json:"type"`
		Key         string          `json:"key"`
		RepeatAfter string          `json:"repeat-after"`
		DataJSON    json.RawMessage `json:"data"`
	}
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&payload); err != nil {
		return BadRequest("cannot decode request body: %v", err)
	}

	if payload.Action != "add" {
		return BadRequest("invalid action %q", payload.Action)
	}
	if payload.Type != "custom" {
		return BadRequest(`invalid type %q (can only add "custom" notices)`, payload.Type)
	}
	if !customKeyRegexp.MatchString(payload.Key) {
		return BadRequest(`invalid key %q (must be in "example.com/path" format)`, payload.Key)
	}
	if len(payload.Key) > maxNoticeKeyLength {
		return BadRequest("key must be %d bytes or less", maxNoticeKeyLength)
	}

	repeatAfter, err := parseOptionalDuration(payload.RepeatAfter)
	if err != nil {
		return BadRequest("invalid repeat-after: %v", err)
	}

	if len(payload.DataJSON) > maxNoticeDataSize {
		return BadRequest("total size of data must be %d bytes or less", maxNoticeDataSize)
	}
	var data map[string]string
	if len(payload.DataJSON) > 0 {
		err = json.Unmarshal(payload.DataJSON, &data)
		if err != nil {
			return BadRequest("cannot decode notice data: %v", err)
		}
	}

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	noticeId, err := st.AddNotice(user.UID, state.CustomNotice, payload.Key, &state.AddNoticeOptions{
		Data:        data,
		RepeatAfter: repeatAfter,
	})
	if err != nil {
		return InternalError("%v", err)
	}

	return SyncResponse(addedNotice{ID: noticeId})
}

func v1GetNotice(c *Command, r *http.Request, user *UserState) Response {
	if user == nil || user.UID == nil {
		return Forbidden("cannot determine UID of request, so cannot retrieve notice")
	}
	noticeID := muxVars(r)["id"]
	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()
	notice := st.Notice(noticeID)
	if notice == nil {
		return NotFound("cannot find notice with ID %q", noticeID)
	}
	if !noticeViewableByUser(notice, user) {
		return Forbidden("not allowed to access notice with id %q", noticeID)
	}
	return SyncResponse(notice)
}

func noticeViewableByUser(notice *state.Notice, user *UserState) bool {
	userID, isSet := notice.UserID()
	if !isSet {
		// Notice has no UID, so it's viewable by any user (with a UID).
		return true
	}
	if user.Access == state.AdminAccess {
		// User is admin, they can view anything.
		return true
	}
	// Otherwise user's UID must match notice's UID.
	return *user.UID == userID
}
