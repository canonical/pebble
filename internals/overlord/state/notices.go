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

package state

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/canonical/pebble/internals/logger"
)

const MaxNoticeKeyLength = 255

// Notice represents an aggregated notice. The combination of Type and Key is unique.
type Notice struct {
	// Server-generated unique ID for this notice (a surrogate key). Users
	// shouldn't rely on this, but this will be a monotonically increasing
	// number (like change ID).
	id string

	// The notice's type.
	noticeType NoticeType

	// The notice key: a string that must be unique for this notice type.
	// For example, for the "change-update" notice, the key is the change ID.
	// For a warning, the key would be the warning message.
	//
	// This is limited to a maximum of 255 bytes when added (it's an error
	// to add a notice with a longer key).
	key string

	// The first time one of these notices (Type and Key combination) occurred.
	firstOccurred time.Time

	// The last time one of these notices occurred.
	lastOccurred time.Time

	// Same as lastOccurred when the notice was last repeated. Only updated
	// once lastOccurred > lastRepeated + repeatAfter.
	lastRepeated time.Time

	// The number of times one of these notices has occurred.
	occurrences int

	// Additional data for the last occurrence of this Type and Key combination.
	lastData map[string]string

	// How much time after one of these last occurred should we allow it to repeat.
	repeatAfter time.Duration

	// How much time since one of these last occurred should we drop the notice.
	expireAfter time.Duration
}

// expired reports whether this notice has expired (relative to the given "now").
func (n *Notice) expired(now time.Time) bool {
	return n.lastOccurred.Add(n.expireAfter).Before(now)
}

// jsonNotice exists so we can control how a Notice is marshalled to JSON. It
// needs to live in this package (rather than the API) because we save state
// to disk as JSON.
type jsonNotice struct {
	ID            string            `json:"id"`
	Type          string            `json:"type"`
	Key           string            `json:"key"`
	FirstOccurred time.Time         `json:"first-occurred"`
	LastOccurred  time.Time         `json:"last-occurred"`
	LastRepeated  time.Time         `json:"last-repeated"`
	Occurrences   int               `json:"occurrences"`
	LastData      map[string]string `json:"last-data,omitempty"`
	RepeatAfter   string            `json:"repeat-after,omitempty"`
	ExpireAfter   string            `json:"expire-after,omitempty"`
}

func (n *Notice) MarshalJSON() ([]byte, error) {
	jn := jsonNotice{
		ID:            n.id,
		Type:          string(n.noticeType),
		Key:           n.key,
		FirstOccurred: n.firstOccurred,
		LastOccurred:  n.lastOccurred,
		LastRepeated:  n.lastRepeated,
		Occurrences:   n.occurrences,
		LastData:      n.lastData,
	}
	if n.repeatAfter != 0 {
		jn.RepeatAfter = n.repeatAfter.String()
	}
	if n.expireAfter != 0 {
		jn.ExpireAfter = n.expireAfter.String()
	}
	return json.Marshal(jn)
}

func (n *Notice) UnmarshalJSON(data []byte) error {
	var jn jsonNotice
	err := json.Unmarshal(data, &jn)
	if err != nil {
		return err
	}
	n.id = jn.ID
	n.noticeType = NoticeType(jn.Type)
	n.key = jn.Key
	n.firstOccurred = jn.FirstOccurred
	n.lastOccurred = jn.LastOccurred
	n.lastRepeated = jn.LastRepeated
	n.occurrences = jn.Occurrences
	n.lastData = jn.LastData
	if jn.RepeatAfter != "" {
		n.repeatAfter, err = time.ParseDuration(jn.RepeatAfter)
		if err != nil {
			return fmt.Errorf("invalid repeat-after duration: %w", err)
		}
	}
	if jn.ExpireAfter != "" {
		n.expireAfter, err = time.ParseDuration(jn.ExpireAfter)
		if err != nil {
			return fmt.Errorf("invalid expire-after duration: %w", err)
		}
	}
	return nil
}

type NoticeType string

const (
	// Recorded whenever a change is updated: when it is first spawned or its
	// status was updated.
	NoticeChangeUpdate NoticeType = "change-update"

	// A client notice reported via the Pebble client API or "pebble notify".
	// The key and data fields are provided by the user. The key must be in
	// the format "mydomain.io/mykey" to ensure well-namespaced notice keys.
	NoticeClient NoticeType = "client"

	// Warnings are a subset of notices where the key is a human-readable
	// warning message.
	NoticeWarning NoticeType = "warning"
)

// NoticeTypeFromString validates the given string and returns the NoticeType,
// or empty string if it's not valid.
func NoticeTypeFromString(s string) NoticeType {
	noticeType := NoticeType(s)
	switch noticeType {
	case NoticeChangeUpdate, NoticeClient, NoticeWarning:
		return noticeType
	default:
		return ""
	}
}

// AddNotice adds an occurrence of a notice with the specified type and key
// and key-value data.
func (s *State) AddNotice(noticeType NoticeType, key string, data map[string]string, repeatAfter time.Duration) {
	if noticeType == "" || key == "" || len(key) > MaxNoticeKeyLength {
		// Programming error
		logger.Panicf("Internal error, please report: attempted to add invalid notice (type %q, key %q)",
			noticeType, key)
	}

	s.writing()

	now := time.Now().UTC()
	uniqueKey := uniqueNoticeKey(noticeType, key)
	notice, ok := s.notices[uniqueKey]
	newOrRepeated := false
	if !ok {
		s.noticeId++
		notice = &Notice{
			id:            strconv.Itoa(s.noticeId),
			noticeType:    noticeType,
			key:           key,
			firstOccurred: now,
			lastRepeated:  now,
			repeatAfter:   repeatAfter,
			expireAfter:   7 * 24 * time.Hour, // one week
			occurrences:   1,
		}
		s.notices[uniqueKey] = notice
		newOrRepeated = true
	} else {
		notice.occurrences++
		if repeatAfter != 0 && now.After(notice.lastRepeated.Add(repeatAfter)) {
			// Update last repeated time if repeat-after time has elapsed
			notice.lastRepeated = now
			newOrRepeated = true
		}
	}
	notice.lastOccurred = now
	notice.lastData = data
	notice.repeatAfter = repeatAfter

	if newOrRepeated {
		s.processNoticeWaiters()
	}
}

func uniqueNoticeKey(noticeType NoticeType, key string) string {
	return string(noticeType) + ":" + key
}

// NoticeFilters allows callers to filter Notices() by various fields.
type NoticeFilters struct {
	// Type, if set, includes only notices of this type.
	Type NoticeType
	// Key, if set, includes only notices with this key.
	Key string
	// After, if set, includes only notices that were last repeated after this time.
	After time.Time
}

func (f *NoticeFilters) matches(n *Notice) bool {
	if f.Type != "" && f.Type != n.noticeType {
		return false
	}
	if f.Key != "" && f.Key != n.key {
		return false
	}
	if !f.After.IsZero() && !n.lastRepeated.After(f.After) {
		return false
	}
	return true
}

// Notices returns the list of notices that match the filters (if any),
// ordered by the last-repeated time.
func (s *State) Notices(filters NoticeFilters) []*Notice {
	s.reading()

	notices := s.flattenNotices(filters)
	sort.Slice(notices, func(i, j int) bool {
		return notices[i].lastRepeated.Before(notices[j].lastRepeated)
	})
	return notices
}

func (s *State) flattenNotices(filters NoticeFilters) []*Notice {
	now := time.Now()
	var notices []*Notice
	for _, n := range s.notices {
		if n.expired(now) || !filters.matches(n) {
			continue
		}
		notices = append(notices, n)
	}
	return notices
}

func (s *State) unflattenNotices(flat []*Notice) {
	now := time.Now()
	s.notices = make(map[string]*Notice)
	maxNoticeId := 0
	for _, n := range flat {
		if n.expired(now) {
			continue
		}
		uniqueKey := uniqueNoticeKey(n.noticeType, n.key)
		s.notices[uniqueKey] = n

		// Evaluate the highest ID and start noticeId state from there.
		noticeId, err := strconv.Atoi(n.id)
		if err != nil {
			logger.Panicf("Internal error: invalid notice ID %q: %v", n.id, err)
		}
		if noticeId > maxNoticeId {
			maxNoticeId = noticeId
		}
	}
	s.noticeId = maxNoticeId
}

// WaitNotices waits for new notices that match the filters to occur or be
// repeated, returning the list of matching notices ordered by the
// last-repeated time. It waits till there is at least one matching notice or
// the context times out or is cancelled.
func (s *State) WaitNotices(ctx context.Context, filters NoticeFilters) ([]*Notice, error) {
	ch, waiterId := s.addNoticeWaiter(filters, ctx.Done())
	defer s.removeNoticeWaiter(waiterId)

	// Unlock state while waiting
	s.Unlock()
	defer s.Lock()

	for {
		select {
		case notices := <-ch:
			return notices, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

type noticeWaiter struct {
	filters NoticeFilters
	ch      chan []*Notice
	done    <-chan struct{}
}

func (s *State) addNoticeWaiter(filters NoticeFilters, done <-chan struct{}) (ch chan []*Notice, waiterId int) {
	s.noticeWaitersMu.Lock()
	defer s.noticeWaitersMu.Unlock()

	s.noticeWaiterId++
	waiterId = s.noticeWaiterId
	waiter := noticeWaiter{
		filters: filters,
		ch:      make(chan []*Notice),
		done:    done,
	}
	s.noticeWaiters[waiterId] = waiter
	return waiter.ch, waiterId
}

func (s *State) removeNoticeWaiter(waiterId int) {
	s.noticeWaitersMu.Lock()
	defer s.noticeWaitersMu.Unlock()

	delete(s.noticeWaiters, waiterId)
}

func (s *State) processNoticeWaiters() {
	s.noticeWaitersMu.Lock()
	defer s.noticeWaitersMu.Unlock()

	for _, waiter := range s.noticeWaiters {
		notices := s.Notices(waiter.filters)
		if len(notices) > 0 {
			select {
			case waiter.ch <- notices:
				// Got matching notices, send them to related WaitNotices
			case <-waiter.done:
				// Will happen if WaitNotices times out
			}
		}
	}
}
