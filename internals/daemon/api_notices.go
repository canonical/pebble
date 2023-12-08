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

	"github.com/canonical/pebble/internals/logger"
	"github.com/canonical/pebble/internals/overlord/state"
)

// Ensure custom keys are in the form "domain.com/key" (but somewhat more restrictive).
var customKeyRegexp = regexp.MustCompile(
	`^[a-z0-9]+(-[a-z0-9]+)*(\.[a-z0-9]+(-[a-z0-9]+)*)+(/[a-z0-9]+(-[a-z0-9]+)*)+$`)

const (
	maxNoticeKeyLength = 256
	maxNoticeDataSize  = 4 * 1024
)

var (
	errVisibilitiesFilterNoNotices = errors.New("all requested visibilities invalid")
)

type addedNotice struct {
	ID string `json:"id"`
}

func v1GetNotices(c *Command, r *http.Request, _ *UserState) Response {
	query := r.URL.Query()

	publicOnly := false
	requestUID, err := uidFromRequest(r)
	daemonUID := uint32(sysGetuid())
	if err != nil {
		// Only allow connection to receive public notices
		publicOnly = true
	}

	userIDs, includeAllPrivate, err := sanitizeUserIDsFilter(requestUID, daemonUID, query["user-ids"])
	if err != nil {
		return statusBadRequest(`invalid "user-ids" filter: %v`, err)
	}

	types, err := sanitizeTypesFilter(query["types"])
	if err != nil {
		// Caller did provide a types filter, but they're all invalid notice types.
		// Return no notices, rather than the default of all notices.
		return SyncResponse([]*state.Notice{})
	}

	keys := strutil.MultiCommaSeparatedList(query["keys"])

	visibilities, err := sanitizeVisibilitiesFilter(query["visibilities"])
	if errors.Is(err, errVisibilitiesFilterNoNotices) {
		// Visibilities filter precludes any possible notices, so return an
		// empty list, rather than locking the state and checking all notices.
		return SyncResponse([]*state.Notice{})
	} else if err != nil {
		return statusBadRequest(`invalid "visibilities" filter: %v`, err)
	}

	after, err := parseOptionalTime(query.Get("after"))
	if err != nil {
		return statusBadRequest(`invalid "after" timestamp: %v`, err)
	}

	filter := &state.NoticeFilter{
		UserIDs:      userIDs,
		Types:        types,
		Keys:         keys,
		Visibilities: visibilities,
		After:        after,
	}

	timeout, err := parseOptionalDuration(query.Get("timeout"))
	if err != nil {
		return statusBadRequest("invalid timeout: %v", err)
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

	viewable := []*state.Notice{}
	for _, n := range notices {
		if noticeViewableByUser(n, requestUID, daemonUID, publicOnly) && includeNotice(n, requestUID, userIDs, includeAllPrivate) {
			viewable = append(viewable, n)
		}
	}
	return SyncResponse(viewable)
}

// Get the UID of the request. If the UID is not known, return an error.
func uidFromRequest(r *http.Request) (uint32, error) {
	_, uid, _, err := ucrednetGet(r.RemoteAddr)
	if err != nil {
		return 0, fmt.Errorf("could not parse request UID")
	}
	return uid, nil
}

// Construct the user IDs filter which will be passed to state.Notices.
// The userID value of "self" means the requester UID.
// The userID value of "all" (admin only) means all public and private notices
// for all users. If "all" is in the query parameters, return true along with
// the parsed user IDs.
func sanitizeUserIDsFilter(requestUID, daemonUID uint32, queryUserIDs []string) (userIDs []uint32, includeAllPrivate bool, err error) {
	userIDStrs := strutil.MultiCommaSeparatedList(queryUserIDs)
	userIDs = make([]uint32, 0, len(userIDStrs))
	for _, userIDStr := range userIDStrs {
		if userIDStr == "self" {
			userIDs = append(userIDs, requestUID)
			continue
		}
		if userIDStr == "all" {
			if isAdmin(requestUID, daemonUID) {
				includeAllPrivate = true
			} else {
				// Don't return error, but log it, in case client is under the
				// incorrect assumption that they are admin. Still Return an
				// empty userIDs filter, so they'll get as many notices as they
				// have permission to view.
				logger.Noticef(`notices: non-admin user %d requested user-ids="all"`, requestUID)
			}
			// Return an empty userIDs filter, so all notices will be returned.
			return []uint32{}, includeAllPrivate, nil
		}
		userID, err := strconv.ParseInt(userIDStr, 10, 64)
		if err != nil {
			return nil, false, err
		}
		if userID < 0 || userID > math.MaxUint32 {
			return nil, false, fmt.Errorf("user ID is not a valid uint32: %d", userID)
		}
		userIDs = append(userIDs, uint32(userID))
	}
	return userIDs, includeAllPrivate, nil
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

// Construct the visibilities filter which will be passed to state.Notices.
func sanitizeVisibilitiesFilter(queryVisibilities []string) ([]state.NoticeVisibility, error) {
	visibilityStrs := strutil.MultiCommaSeparatedList(queryVisibilities)
	visibilities := make([]state.NoticeVisibility, 0, len(visibilityStrs))
	for _, v := range visibilityStrs {
		visibility := state.NoticeVisibility(v)
		if visibility.Valid() {
			visibilities = append(visibilities, visibility)
		}
	}
	if len(visibilities) == 0 && len(visibilityStrs) > 0 {
		return nil, errVisibilitiesFilterNoNotices
	}
	return visibilities, nil
}

func noticeViewableByUser(notice *state.Notice, requestUID, daemonUID uint32, publicOnly bool) bool {
	if notice.Visibility() == state.PublicNotice {
		return true
	}
	if publicOnly {
		// IMPORTANT: must run this check before UID or admin check, as
		// requests with unknown UIDs get set arbitrarily to UID 0.
		return false
	}
	if notice.UserID() == requestUID {
		return true
	}
	if isAdmin(requestUID, daemonUID) {
		return true
	}
	return false
}

func includeNotice(notice *state.Notice, requestUID uint32, userIDs []uint32, includeAllPrivate bool) bool {
	if notice.Visibility() == state.PublicNotice {
		return true
	}
	if notice.UserID() == requestUID {
		return true
	}
	if len(userIDs) == 0 && includeAllPrivate {
		// No user IDs filter provided, only include other users' private
		// notices if includeAllPrivate is true.
		return true
	}
	if sliceContains(userIDs, notice.UserID()) {
		return true
	}
	return false
}

func isAdmin(userID, daemonUID uint32) bool {
	return userID == 0 || userID == daemonUID
}

func sliceContains[T comparable](haystack []T, needle T) bool {
	for _, v := range haystack {
		if v == needle {
			return true
		}
	}
	return false
}

func v1PostNotices(c *Command, r *http.Request, _ *UserState) Response {
	requestUID, err := uidFromRequest(r)
	daemonUID := uint32(sysGetuid())
	if err != nil {
		// Connection UID cannot be parsed, so do not allow notice creation
		return statusBadRequest("cannot determine UID of request, so cannot create notice")
	}

	var payload struct {
		Action      string                 `json:"action"`
		Type        string                 `json:"type"`
		Key         string                 `json:"key"`
		Visibility  state.NoticeVisibility `json:"visibility"`
		RepeatAfter string                 `json:"repeat-after"`
		DataJSON    json.RawMessage        `json:"data"`
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

	if err := validateVisibilityByUser(payload.Visibility, requestUID, daemonUID); err != nil {
		return statusBadRequest(`invalid visibility %q: %v`, payload.Visibility, err)
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

	noticeId, err := st.AddNotice(requestUID, state.CustomNotice, payload.Key, &state.AddNoticeOptions{
		Visibility:  payload.Visibility,
		Data:        data,
		RepeatAfter: repeatAfter,
	})
	if err != nil {
		return statusInternalError("%v", err)
	}

	return SyncResponse(addedNotice{ID: noticeId})
}

func validateVisibilityByUser(visibility state.NoticeVisibility, requestUID, daemonUID uint32) error {
	if visibility == "" {
		return nil
	}
	if !visibility.Valid() {
		return fmt.Errorf("must be %q or %q", state.PrivateNotice, state.PublicNotice)
	}
	if visibility == state.PublicNotice && !isAdmin(requestUID, daemonUID) {
		return fmt.Errorf("only admin may create public notices")
	}
	return nil
}

func v1GetNotice(c *Command, r *http.Request, _ *UserState) Response {
	noticeID := muxVars(r)["id"]
	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()
	notice := st.Notice(noticeID)
	if notice == nil {
		return statusNotFound("cannot find notice with ID %q", noticeID)
	}
	onlyPublic := false
	requestUID, err := uidFromRequest(r)
	if err != nil {
		// Only allow connection to receive public notices
		onlyPublic = true
	}
	daemonUID := uint32(sysGetuid())
	if !noticeViewableByUser(notice, requestUID, daemonUID, onlyPublic) {
		return statusForbidden("not allowed to access notice with id %q", noticeID)
	}
	return SyncResponse(notice)
}
