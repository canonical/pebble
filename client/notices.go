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
	"net/http"
	"net/url"
	"time"
)

type NotifyOptions struct {
	// Key is the client notice's key. Must be in "domain.com/key" format.
	Key string

	// RepeatAfter, if provided, allows the notice to repeat after this duration.
	RepeatAfter time.Duration

	// Data are optional key=value pairs for this occurrence of the notice.
	Data map[string]string
}

// Notify records an occurrence of a "client" notice with the specified options.
func (client *Client) Notify(opts *NotifyOptions) error {
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
		return err
	}
	_, err := client.doSync("POST", "/v1/notices", nil, nil, &body, nil)
	return err
}

type NoticesOptions struct {
	// Type, if set, includes only notices of this type.
	Type string
	// Key, if set, includes only notices with this key.
	Key string
	// After, if set, includes only notices that were last repeated after this time.
	After time.Time
}

// Notice is a notification whose identity is the combination of Type and Key
// that has occurred Occurrences number of times.
type Notice struct {
	ID            string            `json:"id"`
	Type          string            `json:"type"`
	Key           string            `json:"key"`
	FirstOccurred time.Time         `json:"first-occurred"`
	LastOccurred  time.Time         `json:"last-occurred"`
	LastRepeated  time.Time         `json:"last-repeated"`
	Occurrences   int               `json:"occurrences"`
	LastData      map[string]string `json:"last-data,omitempty"`
	RepeatAfter   time.Duration     `json:"repeat-after,omitempty"`
	ExpireAfter   time.Duration     `json:"expire-after,omitempty"`
}

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
// waiting up to the given timeout. They are ordered by the last-repeated time.
func (client *Client) WaitNotices(ctx context.Context, opts *NoticesOptions, timeout time.Duration) ([]*Notice, error) {
	query := makeNoticesQuery(opts)
	query.Set("timeout", timeout.String())

	res, err := client.raw(ctx, "GET", "/v1/notices", query, nil, nil)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusGatewayTimeout {
		return nil, nil
	}

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
	if opts.Type != "" {
		query.Set("type", opts.Type)
	}
	if opts.Key != "" {
		query.Set("key", opts.Key)
	}
	if !opts.After.IsZero() {
		query.Set("after", opts.After.Format(time.RFC3339))
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
