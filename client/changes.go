// Copyright (c) 2014-2020 Canonical Ltd
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
	"time"
)

// Change is a modification to the system state.
type Change struct {
	ID      string  `json:"id"`
	Kind    string  `json:"kind"`
	Summary string  `json:"summary"`
	Status  string  `json:"status"`
	Tasks   []*Task `json:"tasks,omitempty"`
	Ready   bool    `json:"ready"`
	Err     string  `json:"err,omitempty"`

	SpawnTime time.Time `json:"spawn-time,omitempty"`
	ReadyTime time.Time `json:"ready-time,omitempty"`

	data map[string]*json.RawMessage
}

// ErrNoData is returned when there is no data associated with a given key.
var ErrNoData = fmt.Errorf("data entry not found")

// Get unmarshals into value the kind-specific data with the provided key.
func (c *Change) Get(key string, value any) error {
	raw := c.data[key]
	if raw == nil {
		return ErrNoData
	}
	return json.Unmarshal(*raw, value)
}

// Task represents a single operation done to change the system's state.
type Task struct {
	ID       string       `json:"id"`
	Kind     string       `json:"kind"`
	Summary  string       `json:"summary"`
	Status   string       `json:"status"`
	Log      []string     `json:"log,omitempty"`
	Progress TaskProgress `json:"progress"`

	SpawnTime time.Time `json:"spawn-time,omitempty"`
	ReadyTime time.Time `json:"ready-time,omitempty"`

	Data map[string]*json.RawMessage
}

// Get unmarshals into value the kind-specific data with the provided key.
func (t *Task) Get(key string, value any) error {
	raw := t.Data[key]
	if raw == nil {
		return ErrNoData
	}
	return json.Unmarshal(*raw, value)
}

// TaskProgress represents the completion progress of a task.
type TaskProgress struct {
	Label string `json:"label"`
	Done  int    `json:"done"`
	Total int    `json:"total"`
}

type changeAndData struct {
	Change
	Data map[string]*json.RawMessage `json:"data"`
}

// This is used to ensure we send a well-formed change ID in the URL path.
// It's a little more permissive than the currently-valid change IDs (which
// are always integers), but it will allow older clients to talk to newer
// servers which might start allowing letters too (for example).
var changeIDRegexp = regexp.MustCompile(`^[a-z0-9]+$`)

// Change fetches information about a Change given its ID.
func (client *Client) Change(id string) (*Change, error) {
	if !changeIDRegexp.MatchString(id) {
		return nil, fmt.Errorf("invalid change ID %q", id)
	}

	var chgd changeAndData
	resp, err := client.Requester().Do(context.Background(), &RequestOptions{
		Type:   SyncRequest,
		Method: "GET",
		Path:   "/v1/changes/" + id,
	})
	if err != nil {
		return nil, err
	}
	err = resp.DecodeResult(&chgd)
	if err != nil {
		return nil, err
	}

	chgd.Change.data = chgd.Data
	return &chgd.Change, nil
}

// Abort attempts to abort a change that is not yet ready.
func (client *Client) Abort(id string) (*Change, error) {
	if !changeIDRegexp.MatchString(id) {
		return nil, fmt.Errorf("invalid change ID %q", id)
	}

	var postData struct {
		Action string `json:"action"`
	}
	postData.Action = "abort"

	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(postData); err != nil {
		return nil, err
	}

	var chg Change
	resp, err := client.Requester().Do(context.Background(), &RequestOptions{
		Type:   SyncRequest,
		Method: "POST",
		Path:   "/v1/changes/" + id,
		Body:   &body,
	})
	if err != nil {
		return nil, err
	}
	err = resp.DecodeResult(&chg)
	if err != nil {
		return nil, err
	}

	return &chg, nil
}

// ChangeSelector represents a selection of changes to query for.
type ChangeSelector uint8

func (c ChangeSelector) String() string {
	switch c {
	case ChangesInProgress:
		return "in-progress"
	case ChangesReady:
		return "ready"
	case ChangesAll:
		return "all"
	}

	panic(fmt.Sprintf("unknown ChangeSelector %d", c))
}

const (
	ChangesInProgress ChangeSelector = 1 << iota
	ChangesReady
	ChangesAll = ChangesReady | ChangesInProgress
)

type ChangesOptions struct {
	ServiceName string // if empty, no filtering by service is done
	Selector    ChangeSelector
}

// Changes fetches information for the changes specified.
func (client *Client) Changes(opts *ChangesOptions) ([]*Change, error) {
	query := url.Values{}
	if opts != nil {
		if opts.Selector != 0 {
			query.Set("select", opts.Selector.String())
		}
		if opts.ServiceName != "" {
			query.Set("for", opts.ServiceName)
		}
	}

	var chgds []changeAndData
	resp, err := client.Requester().Do(context.Background(), &RequestOptions{
		Type:   SyncRequest,
		Method: "GET",
		Path:   "/v1/changes",
		Query:  query,
	})
	if err != nil {
		return nil, err
	}
	err = resp.DecodeResult(&chgds)
	if err != nil {
		return nil, err
	}

	var chgs []*Change
	for i := range chgds {
		chgd := &chgds[i]
		chgd.Change.data = chgd.Data
		chgs = append(chgs, &chgd.Change)
	}

	return chgs, err
}

type WaitChangeOptions struct {
	// If nonzero, wait at most this long before returning. If a timeout
	// occurs, WaitChange will return an error.
	Timeout time.Duration
}

// WaitChange waits for the change to be finished. If the wait operation
// succeeds, the returned Change.Err string will be non-empty if the change
// itself had an error.
func (client *Client) WaitChange(id string, opts *WaitChangeOptions) (*Change, error) {
	if !changeIDRegexp.MatchString(id) {
		return nil, fmt.Errorf("invalid change ID %q", id)
	}

	var chgd changeAndData

	query := url.Values{}
	if opts != nil && opts.Timeout != 0 {
		query.Set("timeout", opts.Timeout.String())
	}

	resp, err := client.Requester().Do(context.Background(), &RequestOptions{
		Type:   SyncRequest,
		Method: "GET",
		Path:   "/v1/changes/" + id + "/wait",
		Query:  query,
	})
	if err != nil {
		return nil, err
	}
	err = resp.DecodeResult(&chgd)
	if err != nil {
		return nil, err
	}

	chgd.Change.data = chgd.Data
	return &chgd.Change, nil
}
