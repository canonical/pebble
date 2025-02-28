// Copyright (c) 2025 Canonical Ltd
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a Copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package workloads

import (
	"errors"
	"maps"
	"reflect"

	"github.com/canonical/pebble/internals/osutil"
	"github.com/canonical/pebble/internals/plan"
)

type Workload struct {
	// Basic details
	Name     string        `yaml:"-"`
	Override plan.Override `yaml:"override,omitempty"`

	// Options for command execution
	Environment map[string]string `yaml:"environment,omitempty"`
	UserID      *int              `yaml:"user-id,omitempty"`
	User        string            `yaml:"user,omitempty"`
	GroupID     *int              `yaml:"group-id,omitempty"`
	Group       string            `yaml:"group,omitempty"`
}

func (w *Workload) Validate() error {
	if w.Name == "" {
		return errors.New("cannot have an empty name")
	}
	// Value of Override is checked in the (*WorkloadSection).combine() method
	return nil
}

func (w *Workload) Copy() *Workload {
	copied := *w
	copied.Environment = maps.Clone(w.Environment)
	copied.UserID = copyPtr(w.UserID)
	copied.GroupID = copyPtr(w.GroupID)
	return &copied
}

func (w *Workload) Merge(other *Workload) {
	if len(other.Environment) > 0 {
		w.Environment = makeMapIfNil(w.Environment)
		maps.Copy(w.Environment, other.Environment)
	}
	if other.UserID != nil {
		w.UserID = copyPtr(other.UserID)
	}
	if other.User != "" {
		w.User = other.User
	}
	if other.GroupID != nil {
		w.GroupID = copyPtr(other.GroupID)
	}
	if other.Group != "" {
		w.Group = other.Group
	}
}

func (w *Workload) Equal(other *Workload) bool {
	if !maps.Equal(w.Environment, other.Environment) {
		return false
	}

	uid, gid, err := osutil.NormalizeUidGid(w.UserID, w.GroupID, w.User, w.Group)
	if err != nil {
		// If we can't normalize them (shouldn't happen in practice), fall back to
		// deeply comparing whether the values are equal.
		return reflect.DeepEqual(w, other)
	}
	otherUID, otherGID, err := osutil.NormalizeUidGid(other.UserID, other.GroupID, other.User, other.Group)
	if err != nil {
		return reflect.DeepEqual(w, other)
	}
	if uid != nil && gid != nil && otherUID != nil && otherGID != nil {
		return *uid == *otherUID && *gid == *otherGID
	}
	return reflect.DeepEqual(w, other)
}

func copyPtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	copied := *p
	return &copied
}

func makeMapIfNil[K comparable, V any](m map[K]V) map[K]V {
	if m == nil {
		m = make(map[K]V)
	}
	return m
}
