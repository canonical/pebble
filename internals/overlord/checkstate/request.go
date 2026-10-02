// Copyright (c) 2024 Canonical Ltd
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

package checkstate

import (
	"context"
	"fmt"

	"github.com/canonical/pebble/internals/overlord/state"
	"github.com/canonical/pebble/internals/plan"
)

type checkDetails struct {
	Name      string `json:"name"`
	Failures  int    `json:"failures"`
	Successes int    `json:"successes"`
	// Whether to proceed to next check type when change is ready
	Proceed bool `json:"proceed,omitempty"`
}

type performConfigKey struct {
	changeID string
}

// performCheckChange creates the change to perform a check. The change's span
// is a child of the span carried by ctx (for example, the API request that
// started the check), if any, and linked to the previous change for the check
// (identified by prevChangeID), if any.
func performCheckChange(ctx context.Context, st *state.State, config *plan.Check, prevChangeID string) (changeID string) {
	summary := fmt.Sprintf("Perform %s check %q", checkType(config), config.Name)
	task := st.NewTask(performCheckKind, summary)
	task.Set(checkDetailsAttr, &checkDetails{Name: config.Name})

	change := st.NewChangeWithNoticeDataContext(ctx, performCheckKind, task.Summary(), map[string]string{
		"check-name": config.Name,
	})
	linkPrevChange(st, change, prevChangeID)
	change.Set(noPruneAttr, true)
	change.AddTask(task)

	st.Cache(performConfigKey{change.ID()}, config)

	return change.ID()
}

type recoverConfigKey struct {
	changeID string
}

// recoverCheckChange creates the change to recover a failed check. The
// change's span is linked to the previous (failed) change for the check.
func recoverCheckChange(st *state.State, config *plan.Check, successes, failures int, prevChangeID string) (changeID string) {
	summary := fmt.Sprintf("Recover %s check %q", checkType(config), config.Name)
	task := st.NewTask(recoverCheckKind, summary)
	task.Set(checkDetailsAttr, &checkDetails{
		Name:      config.Name,
		Successes: successes,
		Failures:  failures,
	})

	change := st.NewChangeWithNoticeData(recoverCheckKind, task.Summary(), map[string]string{
		"check-name": config.Name,
	})
	linkPrevChange(st, change, prevChangeID)
	change.Set(noPruneAttr, true)
	change.AddTask(task)

	st.Cache(recoverConfigKey{change.ID()}, config)

	return change.ID()
}

// linkPrevChange links the span of change to that of the previous change for
// the same check, so that the sequence of perform and recover changes can be
// followed in a tracing backend without the trace of a flapping check growing
// indefinitely.
func linkPrevChange(st *state.State, change *state.Change, prevChangeID string) {
	if prevChangeID == "" {
		return
	}
	if prev := st.Change(prevChangeID); prev != nil {
		change.AddSpanLink(prev.SpanContext())
	}
}
