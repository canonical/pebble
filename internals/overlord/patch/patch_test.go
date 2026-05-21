// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (c) 2016 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package patch_test

import (
	"fmt"
	"sort"
	"testing"

	"github.com/canonical/tc"

	"github.com/canonical/pebble/internals/overlord/patch"
	"github.com/canonical/pebble/internals/overlord/state"
)

type patchSuite struct{}

func TestPatchSuite(t *testing.T) {
	tc.Run(t, &patchSuite{})
}

func (s *patchSuite) TestInit(c *tc.C) {
	restore := patch.Fake(2, 1, nil)
	defer restore()

	st := state.New(nil)
	patch.Init(st)

	st.Lock()
	defer st.Unlock()
	var patchLevel int
	err := st.Get("patch-level", &patchLevel)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(patchLevel, tc.Equals, 2)

	var patchSublevel int
	c.Assert(st.Get("patch-sublevel", &patchSublevel), tc.IsNil)
	c.Check(patchSublevel, tc.Equals, 1)
}

func (s *patchSuite) TestNothingToDo(c *tc.C) {
	restore := patch.Fake(2, 1, nil)
	defer restore()

	st := state.New(nil)
	st.Lock()
	st.Set("patch-level", 2)
	st.Unlock()
	err := patch.Apply(st)
	c.Assert(err, tc.ErrorIsNil)
}

func (s *patchSuite) TestNoDowngrade(c *tc.C) {
	restore := patch.Fake(2, 0, nil)
	defer restore()

	st := state.New(nil)
	st.Lock()
	st.Set("patch-level", 3)
	st.Unlock()
	err := patch.Apply(st)
	c.Assert(err, tc.ErrorMatches, `cannot downgrade: software version is too old for the current system state \(patch level 3\)`)
}

func (s *patchSuite) TestApply(c *tc.C) {
	p12 := func(st *state.State) error {
		var n int
		st.Get("n", &n)
		st.Set("n", n+1)
		return nil
	}
	p121 := func(st *state.State) error {
		var o int
		st.Get("o", &o)
		st.Set("o", o+1)
		return nil
	}
	p23 := func(st *state.State) error {
		var n int
		st.Get("n", &n)
		st.Set("n", n*10)
		return nil
	}

	// patch level 3, sublevel 1
	restore := patch.Fake(3, 1, map[int][]patch.PatchFunc{
		2: {p12, p121},
		3: {p23},
	})
	defer restore()

	st := state.New(nil)
	st.Lock()
	st.Set("patch-level", 1)
	st.Unlock()
	err := patch.Apply(st)
	c.Assert(err, tc.ErrorIsNil)

	st.Lock()
	defer st.Unlock()

	var level int
	err = st.Get("patch-level", &level)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(level, tc.Equals, 3)

	var sublevel int
	c.Assert(st.Get("patch-sublevel", &sublevel), tc.IsNil)
	c.Check(sublevel, tc.Equals, 0)

	var n, o int
	err = st.Get("n", &n)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(n, tc.Equals, 10)

	c.Assert(st.Get("o", &o), tc.IsNil)
	c.Assert(o, tc.Equals, 1)
}

func (s *patchSuite) TestApplyLevel6(c *tc.C) {
	var sequence []int
	p50 := generatePatchFunc(50, &sequence)
	p60 := generatePatchFunc(60, &sequence)
	p61 := generatePatchFunc(61, &sequence)

	restore := patch.Fake(6, 1, map[int][]patch.PatchFunc{
		5: {p50},
		6: {p60, p61},
	})
	defer restore()

	// simulate the special case where sublevel is introduced for system that's already on patch level 6.
	// only p61 patch should be applied.
	st := state.New(nil)
	st.Lock()
	st.Set("patch-level", 6)
	st.Unlock()
	c.Assert(patch.Apply(st), tc.IsNil)

	st.Lock()
	defer st.Unlock()

	var level, sublevel int
	c.Assert(sequence, tc.DeepEquals, []int{61})
	c.Assert(st.Get("patch-level", &level), tc.IsNil)
	c.Assert(st.Get("patch-sublevel", &sublevel), tc.IsNil)
	c.Check(level, tc.Equals, 6)
	c.Check(sublevel, tc.Equals, 1)
}

func (s *patchSuite) TestApplyFromSublevel(c *tc.C) {
	var sequence []int
	p60 := generatePatchFunc(60, &sequence)
	p61 := generatePatchFunc(61, &sequence)
	p62 := generatePatchFunc(62, &sequence)
	p70 := generatePatchFunc(70, &sequence)
	p71 := generatePatchFunc(71, &sequence)

	restore := patch.Fake(7, 1, map[int][]patch.PatchFunc{
		6: {p60, p61, p62},
		7: {p70, p71},
	})
	defer restore()

	// we'll be patching from 6.0 -> 7.1
	st := state.New(nil)
	st.Lock()
	st.Set("patch-level", 6)
	st.Set("patch-sublevel", 0)
	st.Unlock()
	c.Assert(patch.Apply(st), tc.IsNil)

	st.Lock()

	var level, sublevel int
	c.Assert(st.Get("patch-level", &level), tc.IsNil)
	c.Assert(st.Get("patch-sublevel", &sublevel), tc.IsNil)
	c.Check(level, tc.Equals, 7)
	c.Check(sublevel, tc.Equals, 1)
	c.Assert(sequence, tc.DeepEquals, []int{61, 62, 70, 71})

	// now patching from 7.1 -> 7.2
	sequence = []int{}
	p72 := generatePatchFunc(72, &sequence)
	patch.Fake(7, 2, map[int][]patch.PatchFunc{
		6: {p60, p61, p62},
		7: {p70, p71, p72},
	})

	st.Unlock()
	c.Assert(patch.Apply(st), tc.IsNil)
	c.Assert(sequence, tc.DeepEquals, []int{72})

	st.Lock()
	defer st.Unlock()

	c.Assert(st.Get("patch-level", &level), tc.IsNil)
	c.Assert(st.Get("patch-sublevel", &sublevel), tc.IsNil)
	c.Check(level, tc.Equals, 7)
	c.Check(sublevel, tc.Equals, 2)
}

func (s *patchSuite) TestMissing(c *tc.C) {
	restore := patch.Fake(3, 0, map[int][]patch.PatchFunc{
		3: {func(s *state.State) error { return nil }},
	})
	defer restore()

	st := state.New(nil)
	st.Lock()
	st.Set("patch-level", 1)
	st.Unlock()
	err := patch.Apply(st)
	c.Assert(err, tc.ErrorMatches, `cannot upgrade: software version is too new for the current system state \(patch level 1\)`)
}

func (s *patchSuite) TestDowngradeSublevel(c *tc.C) {
	restore := patch.Fake(3, 1, map[int][]patch.PatchFunc{
		3: {func(s *state.State) error { return nil }},
	})
	defer restore()

	st := state.New(nil)
	st.Lock()
	st.Set("patch-level", 3)
	st.Set("patch-sublevel", 6)
	st.Unlock()

	// we're at patch level 3, sublevel 6 according to state, but the implemented level is 3,1
	c.Assert(patch.Apply(st), tc.IsNil)

	st.Lock()
	defer st.Unlock()
	var level, sublevel int
	c.Assert(st.Get("patch-level", &level), tc.IsNil)
	c.Assert(st.Get("patch-sublevel", &sublevel), tc.IsNil)
	c.Check(level, tc.Equals, 3)
	c.Check(sublevel, tc.Equals, 1)
}

func (s *patchSuite) TestError(c *tc.C) {
	p12 := func(st *state.State) error {
		var n int
		st.Get("n", &n)
		st.Set("n", n+1)
		return nil
	}
	p23 := func(st *state.State) error {
		var n int
		st.Get("n", &n)
		st.Set("n", n*10)
		return fmt.Errorf("boom")
	}
	p34 := func(st *state.State) error {
		var n int
		st.Get("n", &n)
		st.Set("n", n*100)
		return nil
	}
	restore := patch.Fake(3, 0, map[int][]patch.PatchFunc{
		2: {p12},
		3: {p23},
		4: {p34},
	})
	defer restore()

	st := state.New(nil)
	st.Lock()
	st.Set("patch-level", 1)
	st.Unlock()
	err := patch.Apply(st)
	c.Assert(err, tc.ErrorMatches, `cannot patch system state to level 3, sublevel 0: boom`)

	st.Lock()
	defer st.Unlock()

	var level int
	err = st.Get("patch-level", &level)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(level, tc.Equals, 2)

	var n int
	err = st.Get("n", &n)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(n, tc.Equals, 10)
}

func (s *patchSuite) TestSanity(c *tc.C) {
	patches := patch.PatchesForTest()
	levels := make([]int, 0, len(patches))
	for l := range patches {
		levels = append(levels, l)
	}
	sort.Ints(levels)
	// all steps present
	for i, level := range levels {
		c.Check(level, tc.Equals, i+1)
	}
	// ends at implemented patch level
	c.Check(levels[len(levels)-1], tc.Equals, patch.Level)

	// Sublevel matches the number of patches for last Level.
	c.Check(len(patches[patch.Level])-1, tc.Equals, patch.Sublevel)
}

func generatePatchFunc(testValue int, sequence *[]int) patch.PatchFunc {
	return func(st *state.State) error {
		*sequence = append(*sequence, testValue)
		return nil
	}
}
