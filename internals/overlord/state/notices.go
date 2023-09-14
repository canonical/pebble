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

const (
	// MaxNoticeKeyLength is the maximum key length for notices, in bytes.
	MaxNoticeKeyLength = 255

	// Expiry time for notices. Note that the expiry time for snapd warnings
	// is 28 days, but a shorter time for Pebble notices seems appropriate.
	noticeExpireAfter = 7 * 24 * time.Hour
)

// Notice represents an aggregated notice. The combination of type and key is unique.
type Notice struct {
	// Server-generated unique ID for this notice (a surrogate key).
	//
	// Users shouldn't rely on this, but this will be a monotonically
	// increasing number (like change ID).
	id string

	// The notice type represents a group of notices originating from a common
	// source. For example, notices originating from the CLI client have type
	// "client".
	noticeType NoticeType

	// The notice key is a string that differentiates notices of this type.
	// Notices recorded with the type and key of an existing notice count as
	// an occurrence of that notice.
	//
	// This is limited to a maximum of MaxNoticeKeyLength bytes when added
	// (it's an error to add a notice with a longer key).
	key string

	// The first time one of these notices (type and key combination) occurs.
	firstOccurred time.Time

	// The last time one of these notices occurred. This is updated every time
	// one of these notices occurs.
	lastOccurred time.Time

	// The time this notice was last "repeated". This is set when one of these
	// notices first occurs, and updated when it reoccurs at least
	// repeatAfter after the previous lastRepeated time.
	//
	// Notices and WaitNotices return notices ordered by lastRepeated time, so
	// repeated notices will appear at the end of the returned list.
	lastRepeated time.Time

	// The number of times one of these notices has occurred.
	occurrences int

	// Additional data captured from the last occurrence of one of these notices.
	lastData map[string]string

	// How much time after one of these was last repeated should we allow it
	// to repeat.
	repeatAfter time.Duration

	// How much time since one of these last occurred should we drop the notice.
	expireAfter time.Duration
}

func (n *Notice) String() string {
	return fmt.Sprintf("Notice %s (%s:%s)", n.id, n.noticeType, n.key)
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
	// status was updated. The key for change-update notices is the change ID.
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

// AddNotice records an occurrence of a notice with the specified type and key
// and key-value data, returning the notice ID.
//
// The notice's repeatAfter time is set on the first occurrence; a value of
// zero means "never repeat". It is only updated on subsequent occurrences if
// the repeatAfter argument is nonzero.
func (s *State) AddNotice(noticeType NoticeType, key string, data map[string]string, repeatAfter time.Duration) string {
	return s.addNoticeWithTime(time.Now(), noticeType, key, data, repeatAfter)
}

func (s *State) addNoticeWithTime(now time.Time, noticeType NoticeType, key string, data map[string]string, repeatAfter time.Duration) string {
	if noticeType == "" || key == "" || len(key) > MaxNoticeKeyLength {
		// Programming error (max key length has already been checked by API)
		logger.Panicf("Internal error, please report: attempted to add invalid notice (type %q, key %q)",
			noticeType, key)
	}

	s.writing()

	now = now.UTC()
	newOrRepeated := false
	uniqueKey := uniqueNoticeKey(noticeType, key)
	notice, ok := s.notices[uniqueKey]
	if !ok {
		// First occurrence of this notice type+key
		s.lastNoticeId++
		notice = &Notice{
			id:            strconv.Itoa(s.lastNoticeId),
			noticeType:    noticeType,
			key:           key,
			firstOccurred: now,
			lastRepeated:  now,
			repeatAfter:   repeatAfter,
			expireAfter:   noticeExpireAfter,
			occurrences:   1,
		}
		s.notices[uniqueKey] = notice
		newOrRepeated = true
	} else {
		// Additional occurrence, update existing notice
		notice.occurrences++
		if repeatAfter != 0 {
			notice.repeatAfter = repeatAfter
		}
		if notice.repeatAfter != 0 && now.After(notice.lastRepeated.Add(notice.repeatAfter)) {
			// Update last repeated time if repeat-after time has elapsed
			notice.lastRepeated = now
			newOrRepeated = true
		}
	}
	notice.lastOccurred = now
	notice.lastData = data

	if newOrRepeated {
		s.processNoticeWaiters()
	}

	return notice.id
}

func uniqueNoticeKey(noticeType NoticeType, key string) string {
	return string(noticeType) + ":" + key
}

// NoticeFilters allows filter notices by various fields.
type NoticeFilters struct {
	// Type, if set, includes only notices of this type.
	Type NoticeType
	// Key, if set, includes only notices with this key.
	Key string
	// After, if set, includes only notices that were last repeated after this time.
	After time.Time
}

// matches reports whether the notice n matches these filters
func (f NoticeFilters) matches(n *Notice) bool {
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

// Notice return a single notice by ID, or nil if not found.
func (s *State) Notice(id string) *Notice {
	s.reading()

	// Could use another map for lookup, but the number of notices will likely
	// be small, and this function is probably only used rarely by the CLI, so
	// performance is unlikely to matter.
	for _, notice := range s.notices {
		if notice.id == id {
			return notice
		}
	}
	return nil
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
	for _, n := range flat {
		if n.expired(now) {
			continue
		}
		uniqueKey := uniqueNoticeKey(n.noticeType, n.key)
		s.notices[uniqueKey] = n
	}
}

// WaitNotices waits for notices that match the filters to exist or occur,
// returning the list of matching notices ordered by the last-repeated time.
//
// It waits till there is at least one matching notice or the context times
// out or is cancelled. If there are existing notices that match the
// filters, WaitNotices will return them immediately.
func (s *State) WaitNotices(ctx context.Context, filters NoticeFilters) ([]*Notice, error) {
	// State is already locked here by the caller, so notices won't be added
	// concurrently.
	notices := s.Notices(filters)
	if len(notices) > 0 {
		return notices, nil // if there are existing notices, return them right away
	}

	// Add a waiter channel for AddNotice to send to when matching notices arrive.
	ch, waiterId := s.addNoticeWaiter(filters, ctx.Done())
	defer s.removeNoticeWaiter(waiterId) // state will be re-locked when this is called

	// Unlock state while waiting, to allow new notices to arrive (all state
	// methods expect the caller to have locked the state before the call).
	s.Unlock()
	defer s.Lock()

	select {
	case notices = <-ch:
		// One or more new notices arrived
		return notices, nil
	case <-ctx.Done(): // sender (processNoticeWaiters) also waits on this done channel
		return nil, ctx.Err()
	}
}

// noticeWaiter tracks a single WaitNotices call.
type noticeWaiter struct {
	filters NoticeFilters
	ch      chan []*Notice
	done    <-chan struct{}
}

// addNoticeWaiter adds a notice-waiter with the given filters. Processing
// notices for this waiter stops when the done channel is closed.
func (s *State) addNoticeWaiter(filters NoticeFilters, done <-chan struct{}) (ch chan []*Notice, waiterId int) {
	s.noticeWaiterId++
	waiter := noticeWaiter{
		filters: filters,
		ch:      make(chan []*Notice),
		done:    done,
	}
	s.noticeWaiters[s.noticeWaiterId] = waiter
	return waiter.ch, s.noticeWaiterId
}

// removeNoticeWaiter removes the notice-waiter with the given ID.
func (s *State) removeNoticeWaiter(waiterId int) {
	delete(s.noticeWaiters, waiterId)
}

// processNoticeWaiters loops through the list of notice-waiters, and wakes up
// and sends to any that match the filters.
func (s *State) processNoticeWaiters() {
	for waiterId, waiter := range s.noticeWaiters {
		notices := s.Notices(waiter.filters)
		if len(notices) == 0 {
			continue // no notices with these filters
		}
		select {
		case waiter.ch <- notices:
			// Got matching notices, send them to related WaitNotices.
			// And remove the waiter so we don't try to send to its channel again
			// if another notice comes in.
			s.removeNoticeWaiter(waiterId)
		case <-waiter.done:
			// Will happen if WaitNotices times out (it also waits on this done channel)
		}
	}
}
