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
	"net/url"
	"time"
)

type NotifyOptions struct {
	// Key is the client notice's key. Must be in "domain.com/key" format.
	Key string

	// Data are optional key=value pairs for this occurrence of the notice.
	Data map[string]string

	// RepeatAfter, if provided, allows the notice to repeat after this duration.
	RepeatAfter time.Duration
}

// Notify records an occurrence of a "client" notice with the specified options,
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
		Type:   "client",
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
	// Type, if set, includes only notices of this type.
	Type NoticeType
	// Key, if set, includes only notices with this key.
	Key string
	// After, if set, includes only notices that were last repeated after this time.
	After time.Time
}

// Notice is a notification whose identity is the combination of Type and Key
// that has occurred Occurrences number of times.
type Notice struct {
	ID            string            `json:"id"`
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
	// Recorded whenever a change is updated: when the change is first spawned
	// or its status is updated.
	NoticeChangeUpdate NoticeType = "change-update"

	// A client notice reported via the Pebble client API or "pebble notify".
	// The key and data fields are provided by the user. The key must be in
	// the format "mydomain.io/mykey" to ensure well-namespaced notice keys.
	NoticeClient NoticeType = "client"

	// Warnings are a subset of notices where the key is a human-readable
	// warning message.
	NoticeWarning NoticeType = "warning"
)

type jsonNotice struct {
	Notice
	RepeatAfter string `json:"repeat-after,omitempty"`
	ExpireAfter string `json:"expire-after,omitempty"`
}

// Notice fetches a single notice by ID.
func (client *Client) Notice(id string) (*Notice, error) {
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

	// We need to use client.raw() here to pass the context through (and we
	// don't want retries).
	res, err := client.raw(ctx, "GET", "/v1/notices", query, nil, nil)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	var rsp response
	err = decodeInto(res.Body, &rsp)
	if err != nil {
		return nil, err
	}
	var jns []*jsonNotice
	_, err = client.finishSync(rsp, &jns)
	return jsonNoticesToNotices(jns), err
}

func makeNoticesQuery(opts *NoticesOptions) url.Values {
	query := make(url.Values)
	if opts == nil {
		return query
	}
	if opts.Type != "" {
		query.Set("type", string(opts.Type))
	}
	if opts.Key != "" {
		query.Set("key", opts.Key)
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
