// Copyright (c) 2025 Canonical Ltd
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

package state_test

import (
	"encoding/json"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/overlord/state"
)

type pairingSuite struct{}

var _ = Suite(&pairingSuite{})

func (p *pairingSuite) TestMarshalEmpty(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	pairing := st.Pairing()
	data, err := json.MarshalIndent(pairing, "", "    ")
	c.Assert(err, IsNil)
	c.Assert(string(data), Equals, `
{
    "is-paired": false
}`[1:])
}

func (p *pairingSuite) TestMarshalValues(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	st.SetIsPaired()

	pairing := st.Pairing()
	data, err := json.MarshalIndent(pairing, "", "    ")
	c.Assert(err, IsNil)
	c.Assert(string(data), Equals, `
{
    "is-paired": true
}`[1:])
}

func (p *pairingSuite) TestUnmarshalValues(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	data := []byte(`
{
    "is-paired": true
}`[1:])
	pairing := st.Pairing()
	err := json.Unmarshal(data, pairing)
	c.Assert(err, IsNil)
	c.Assert(st.IsPaired(), Equals, true)
}
