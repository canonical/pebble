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
	// "example.com/path" format.
	Key string

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
		RepeatAfter string            `json:"repeat-after,omitempty"`
		Data        map[string]string `json:"data,omitempty"`
	}{
		Action: "add",
		Type:   string(opts.Type),
		Key:    opts.Key,
		Data:   opts.Data,
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
	// Users allows returning notices for all users.
	Users NoticesUsers

	// UserID, if set, includes only notices that have this user ID or are public.
	UserID *uint32

	// Types, if not empty, includes only notices whose type is one of these.
	Types []NoticeType

	// Keys, if not empty, includes only notices whose key is one of these.
	Keys []string

	// After, if set, includes only notices that were last repeated after this time.
	After time.Time
}

type NoticesUsers string

const (
	NoticesUsersAll NoticesUsers = "all"
)

// Notice holds details of an event that was observed and reported either
// inside the server itself or externally via the API. Besides the ID field
// itself, the Type and Key fields together also uniquely identify a
// particular notice, and when a new notification is made with matching Type
// and Key, the previous notice is updated appropriately instead of a new one
// being created.
type Notice struct {
	ID            string            `json:"id"`
	UserID        *uint32           `json:"user-id"`
	Type          NoticeType        `json:"type"`
	Key           string            `json:"key"`
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
	// Recorded whenever a change is updated: when it is first spawned or its
	// status was updated. The key for change-update notices is the change ID.
	ChangeUpdateNotice NoticeType = "change-update"

	// A custom notice reported via the Pebble client API or "pebble notify".
	// The key and data fields are provided by the user. The key must be in
	// the format "example.com/path" to ensure well-namespaced notice keys.
	CustomNotice NoticeType = "custom"
)

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
	query := makeNoticesQuery(opts)
	var jns []*jsonNotice
	_, err := client.doSync("GET", "/v1/notices", query, nil, nil, &jns)
	return jsonNoticesToNotices(jns), err
}

// WaitNotices returns a list of notices that match the filters given in opts,
// telling the server to wait up to the given timeout. They are ordered by the
// last-repeated time.
//
// If the timeout elapses before any matching notices arrive, it's not
// considered an error: WaitNotices returns a nil slice and a nil error.
func (client *Client) WaitNotices(ctx context.Context, serverTimeout time.Duration, opts *NoticesOptions) ([]*Notice, error) {
	query := makeNoticesQuery(opts)
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

func makeNoticesQuery(opts *NoticesOptions) url.Values {
	query := make(url.Values)
	if opts == nil {
		return query
	}
	if opts.Users != "" {
		query.Add("users", string(opts.Users))
	}
	if opts.UserID != nil {
		query.Add("user-id", strconv.FormatUint(uint64(*opts.UserID), 10))
	}
	for _, t := range opts.Types {
		query.Add("types", string(t))
	}
	if len(opts.Keys) > 0 {
		query["keys"] = opts.Keys
	}
	if !opts.After.IsZero() {
		query.Set("after", opts.After.Format(time.RFC3339Nano))
	}
	return query
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
