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

package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"time"
)

type NotifyOptions struct {
	// Type is the notice's type. Currently only notices of type CustomNotice
	// can be added.
	Type NoticeType

	// Key is the notice's key. For "custom" notices, this must be in
	// "domain.com/key" format.
	Key string

	// Visibility indicates whether the notice is public (viewable by all users)
	// or private (viewable only by the user with the same user ID as the notice,
	// or by admin). If not set, the default is private.
	Visibility NoticeVisibility

	// Data are optional key=value pairs for this occurrence of the notice.
	Data map[string]string

	// RepeatAfter, if not zero, prevents this notice from being observed if a
	// notification with the same Type and Key has been made recently within
	// the provided duration.
	RepeatAfter time.Duration
}

// Notify records an occurrence of a notice with the specified options,
// returning the notice ID.
func (client *Client) Notify(opts *NotifyOptions) (string, error) {
	var payload = struct {
		Action      string            `json:"action"`
		Type        string            `json:"type"`
		Key         string            `json:"key"`
		Visibility  NoticeVisibility  `json:"visibility,omitempty"`
		RepeatAfter string            `json:"repeat-after,omitempty"`
		Data        map[string]string `json:"data,omitempty"`
	}{
		Action:     "add",
		Type:       string(opts.Type),
		Key:        opts.Key,
		Visibility: opts.Visibility,
		Data:       opts.Data,
	}
	if opts.RepeatAfter != 0 {
		payload.RepeatAfter = opts.RepeatAfter.String()
	}
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(&payload); err != nil {
		return "", err
	}

	result := struct {
		ID string `json:"id"`
	}{}
	_, err := client.doSync("POST", "/v1/notices", nil, nil, &body, &result)
	if err != nil {
		return "", err
	}
	return result.ID, err
}

type NoticesOptions struct {
	// UserIDs, if not empty, includes only notices whose user ID is one of these.
	UserIDs []uint32

	// SpecialUser value "self" adds the client UID to the UserIDs filter in
	// the API, and "all" (admin only) indicates to include all notices (both
	// public and private) for all users.
	SpecialUser NoticeSpecialUser

	// Types, if not empty, includes only notices whose type is one of these.
	Types []NoticeType

	// Keys, if not empty, includes only notices whose key is one of these.
	Keys []string

	// Visibilities, if not empty, includes only notices whose visibility is one of these.
	Visibilities []NoticeVisibility

	// After, if set, includes only notices that were last repeated after this time.
	After time.Time
}

// Parse a user ID option, which may be "all", "self", or a UID, and adjust the
// NoticesOptions values accordingly.
func (o *NoticesOptions) HandleUIDOption(uidOpt string) error {
	switch uidOpt {
	case "":
		// nothing to do
	case string(NoticeUserAll):
		o.SpecialUser = NoticeUserAll
	case string(NoticeUserSelf):
		if o.SpecialUser != NoticeUserAll {
			o.SpecialUser = NoticeUserSelf
		}
	default:
		uid, err := strconv.ParseUint(uidOpt, 10, 32)
		if err != nil {
			return err
		}
		o.UserIDs = append(o.UserIDs, uint32(uid))
	}
	return nil
}

// Notice holds details of an event that was observed and reported either
// inside the server itself or externally via the API. Besides the ID field
// itself, the Type and Key fields together also uniquely identify a
// particular notice, and when a new notification is made with matching Type
// and Key, the previous notice is updated appropriately instead of a new one
// being created.
type Notice struct {
	ID            string            `json:"id"`
	UserID        uint32            `json:"user-id"`
	Type          NoticeType        `json:"type"`
	Key           string            `json:"key"`
	Visibility    NoticeVisibility  `json:"visibility"`
	FirstOccurred time.Time         `json:"first-occurred"`
	LastOccurred  time.Time         `json:"last-occurred"`
	LastRepeated  time.Time         `json:"last-repeated"`
	Occurrences   int               `json:"occurrences"`
	LastData      map[string]string `json:"last-data,omitempty"`
	RepeatAfter   time.Duration     `json:"repeat-after,omitempty"`
	ExpireAfter   time.Duration     `json:"expire-after,omitempty"`
}

type NoticeType string

const (
	// A custom notice reported via the Pebble client API or "pebble notify".
	// The key and data fields are provided by the user. The key must be in
	// the format "mydomain.io/mykey" to ensure well-namespaced notice keys.
	CustomNotice NoticeType = "custom"
)

type NoticeSpecialUser string

const (
	// The "all" special user (admin only) indicates to include all notices for all users.
	NoticeUserAll NoticeSpecialUser = "all"

	// The "self" special user indicates to include the client UID in the user IDs filter.
	NoticeUserSelf NoticeSpecialUser = "self"
)

func (u NoticeSpecialUser) Valid() bool {
	switch u {
	case NoticeUserSelf, NoticeUserAll:
		return true
	}
	return false
}

type NoticeVisibility string

const (
	// A private notice is only viewable by the user with a matching user ID, or by admin.
	PrivateNotice NoticeVisibility = "private"

	// A public notice is viewable by all users.
	PublicNotice NoticeVisibility = "public"
)

func (v NoticeVisibility) Valid() bool {
	switch v {
	case PrivateNotice, PublicNotice:
		return true
	}
	return false
}

type jsonNotice struct {
	Notice
	RepeatAfter string `json:"repeat-after,omitempty"`
	ExpireAfter string `json:"expire-after,omitempty"`
}

// This is used to ensure we send a well-formed notice ID in the URL path.
// It's a little more permissive than the currently-valid notice IDs (which
// are always integers), but it will allow older clients to talk to newer
// servers which might start allowing letters too (for example).
var noticeIDRegexp = regexp.MustCompile(`^[a-z0-9]+$`)

// Notice fetches a single notice by ID.
func (client *Client) Notice(id string) (*Notice, error) {
	if !noticeIDRegexp.MatchString(id) {
		return nil, fmt.Errorf("invalid notice ID %q", id)
	}
	var jn *jsonNotice
	_, err := client.doSync("GET", "/v1/notices/"+id, nil, nil, nil, &jn)
	if err != nil {
		return nil, err
	}
	return jsonNoticeToNotice(jn), nil
}

// Notices returns a list of notices that match the filters given in opts,
// ordered by the last-repeated time.
func (client *Client) Notices(opts *NoticesOptions) ([]*Notice, error) {
	query, err := makeNoticesQuery(opts)
	if err != nil {
		return nil, err
	}
	var jns []*jsonNotice
	_, err = client.doSync("GET", "/v1/notices", query, nil, nil, &jns)
	return jsonNoticesToNotices(jns), err
}

// WaitNotices returns a list of notices that match the filters given in opts,
// telling the server to wait up to the given timeout. They are ordered by the
// last-repeated time.
//
// If the timeout elapses before any matching notices arrive, it's not
// considered an error: WaitNotices returns a nil slice and a nil error.
func (client *Client) WaitNotices(ctx context.Context, serverTimeout time.Duration, opts *NoticesOptions) ([]*Notice, error) {
	query, err := makeNoticesQuery(opts)
	if err != nil {
		return nil, err
	}
	query.Set("timeout", serverTimeout.String())

	resp, err := client.Requester().Do(ctx, &RequestOptions{
		Type:   SyncRequest,
		Method: "GET",
		Path:   "/v1/notices",
		Query:  query,
	})
	if err != nil {
		return nil, err
	}

	var jns []*jsonNotice
	err = resp.DecodeResult(&jns)
	if err != nil {
		return nil, err
	}
	return jsonNoticesToNotices(jns), err
}

func makeNoticesQuery(opts *NoticesOptions) (url.Values, error) {
	query := make(url.Values)
	if opts == nil {
		return query, nil
	}
	for _, uid := range opts.UserIDs {
		query.Add("user-ids", strconv.FormatUint(uint64(uid), 10))
	}
	if opts.SpecialUser != "" {
		if !opts.SpecialUser.Valid() {
			return nil, fmt.Errorf("invalid special user: %q", opts.SpecialUser)
		}
		query.Add("user-ids", string(opts.SpecialUser))
	}
	for _, t := range opts.Types {
		query.Add("types", string(t))
	}
	if len(opts.Keys) > 0 {
		query["keys"] = opts.Keys
	}
	for _, v := range opts.Visibilities {
		if !v.Valid() {
			return nil, fmt.Errorf("invalid visibility: %q", v)
		}
		query.Add("visibilities", string(v))
	}
	if !opts.After.IsZero() {
		query.Set("after", opts.After.Format(time.RFC3339Nano))
	}
	return query, nil
}

func jsonNoticesToNotices(jns []*jsonNotice) []*Notice {
	ns := make([]*Notice, len(jns))
	for i, jn := range jns {
		ns[i] = jsonNoticeToNotice(jn)
	}
	return ns
}

func jsonNoticeToNotice(jn *jsonNotice) *Notice {
	n := &jn.Notice
	n.ExpireAfter, _ = time.ParseDuration(jn.ExpireAfter)
	n.RepeatAfter, _ = time.ParseDuration(jn.RepeatAfter)
	return n
}
