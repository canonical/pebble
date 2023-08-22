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
	"sort"
	"strconv"
	"time"
)

// Notice represents an aggregated notice. The combination of Type and Key is unique.
type Notice struct {
	// Server-generated unique ID for this notice (a surrogate key). Users
	// shouldn't rely on this, but this will be a monotonically increasing
	// number (like change ID).
	ID string

	// The notice's type.
	Type NoticeType

	// The notice key: a string that must be unique for this notice type.
	// For example, for the "change-update" notice, the key is the change ID.
	// For a warning, the key would be the warning message.
	//
	// This is limited to a maximum of 255 bytes when added (it's an error
	// to add a notice with a longer key).
	Key string

	// The first time one of these notices (Type and Key combination) occurred.
	FirstOccurred time.Time

	// The last time one of these notices occurred.
	LastOccurred time.Time

	// The number of times one of these notices has occurred.
	Occurrences int

	// Same as LastOccurred when the notice was last repeated. Only updated
	// once LastOccurred > LastRepeated + RepeatAfter.
	LastRepeated time.Time

	// Additional data for the last occurrence of this Type and Key combination.
	LastData map[string]string

	// How much time after one of these last occurred should we allow it to repeat.
	RepeatAfter time.Duration

	// How much time since one of these last occurred should we drop the notice.
	// TODO: snapd default is 28 days
	expireAfter time.Duration // unclear who should set this, or is it global?
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

func (s *State) AddNotice(noticeType NoticeType, key string, data map[string]string) {
	s.writing()

	now := time.Now() // TODO: UTC?
	uniqueKey := uniqueNoticeKey(noticeType, key)
	notice, ok := s.notices[uniqueKey]
	if !ok {
		s.noticeId++
		notice = &Notice{
			ID:            strconv.Itoa(s.noticeId),
			Type:          noticeType,
			Key:           key,
			FirstOccurred: now,
			LastRepeated:  time.Time{}, // TODO
			RepeatAfter:   0,
			expireAfter:   0,
		}
		s.notices[uniqueKey] = notice
	}
	notice.LastOccurred = now
	notice.Occurrences++
	notice.LastData = data
}

func uniqueNoticeKey(noticeType NoticeType, key string) string {
	return string(noticeType) + ":" + key
}

func (s *State) Notices() []*Notice {
	s.reading()

	notices := s.flattenNotices()
	sort.Slice(notices, func(i, j int) bool {
		return notices[i].LastRepeated.Before(notices[j].LastRepeated)
	})
	return notices
}

func (s *State) flattenNotices() []*Notice {
	notices := make([]*Notice, 0, len(s.warnings))
	for _, n := range s.notices {
		//if w.ExpiredBefore(now) {
		//	continue
		//}
		notices = append(notices, n)
	}
	return notices
}

func (s *State) unflattenNotices(flat []*Notice) {
	// TODO
	// TODO: also set s.noticeId to highest
	s.notices = make(map[string]*Notice, len(flat))
}
