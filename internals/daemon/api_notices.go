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
	"math"
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

func v1GetNotices(c *Command, r *http.Request, _ *UserState) Response {
	query := r.URL.Query()

	requestUID, err := uidFromRequest(r)
	if err != nil {
		return Forbidden("cannot determine UID of request, so cannot retrieve notices")
	}
	daemonUID := uint32(sysGetuid())

	// By default, return notices with the request UID and public notices.
	userID := &requestUID

	if len(query["user-id"]) > 0 {
		if !isAdmin(requestUID, daemonUID) {
			return Forbidden(`only admins may use the "user-id" filter`)
		}
		userID, err = sanitizeUserIDFilter(query["user-id"])
		if err != nil {
			return BadRequest(`invalid "user-id" filter: %v`, err)
		}
	}

	if len(query["select"]) > 0 {
		if !isAdmin(requestUID, daemonUID) {
			return Forbidden(`only admins may use the "select" filter`)
		}
		if len(query["user-id"]) > 0 {
			return BadRequest(`cannot use both "select" and "user-id" parameters`)
		}
		if query.Get("select") != "all" {
			return BadRequest(`invalid "select" filter: must be "all"`)
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

// Get the UID of the request. If the UID is not known, return an error.
func uidFromRequest(r *http.Request) (uint32, error) {
	ucred, err := ucrednetGet(r.RemoteAddr)
	if err != nil {
		return 0, fmt.Errorf("could not parse request UID")
	}
	return ucred.Uid, nil
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
	if len(types) == 0 && len(typeStrs) > 0 {
		return nil, errors.New("all requested notice types invalid")
	}
	return types, nil
}

func isAdmin(requestUID, daemonUID uint32) bool {
	return requestUID == 0 || requestUID == daemonUID
}

func v1PostNotices(c *Command, r *http.Request, _ *UserState) Response {
	requestUID, err := uidFromRequest(r)
	if err != nil {
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
		return BadRequest(`invalid key %q (must be in "domain.com/key" format)`, payload.Key)
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

	noticeId, err := st.AddNotice(&requestUID, state.CustomNotice, payload.Key, &state.AddNoticeOptions{
		Data:        data,
		RepeatAfter: repeatAfter,
	})
	if err != nil {
		return InternalError("%v", err)
	}

	return SyncResponse(addedNotice{ID: noticeId})
}

func v1GetNotice(c *Command, r *http.Request, _ *UserState) Response {
	requestUID, err := uidFromRequest(r)
	if err != nil {
		return Forbidden("cannot determine UID of request, so cannot retrieve notice")
	}
	daemonUID := uint32(sysGetuid())
	noticeID := muxVars(r)["id"]
	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()
	notice := st.Notice(noticeID)
	if notice == nil {
		return NotFound("cannot find notice with ID %q", noticeID)
	}
	if !noticeViewableByUser(notice, requestUID, daemonUID) {
		return Forbidden("not allowed to access notice with id %q", noticeID)
	}
	return SyncResponse(notice)
}

func noticeViewableByUser(notice *state.Notice, requestUID, daemonUID uint32) bool {
	userID, isSet := notice.UserID()
	if !isSet {
		return true
	}
	if isAdmin(requestUID, daemonUID) {
		return true
	}
	return requestUID == userID
}
