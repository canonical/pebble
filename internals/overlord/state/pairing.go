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

package state

import (
	"encoding/json"
)

// Pairing holds state related to client server pairing.
type Pairing struct {
	// isPaired reflects if this server instance ever paired
	// with a client. This flag cannot be cleared once set.
	isPaired bool
}

// IsPaired returns true if the server paired with a client before.
func (s *State) IsPaired() bool {
	s.reading()
	return s.pairing.isPaired
}

// SetIsPaired sets the IsPaired state to true.
func (s *State) SetIsPaired() {
	s.writing()
	s.pairing.isPaired = true
}

type jsonPairing struct {
	IsPaired bool `json:"is-paired"`
}

func (p Pairing) MarshalJSON() ([]byte, error) {
	jp := jsonPairing{
		IsPaired: p.isPaired,
	}
	return json.Marshal(jp)
}

func (p *Pairing) UnmarshalJSON(data []byte) error {
	var jp jsonPairing
	err := json.Unmarshal(data, &jp)
	if err != nil {
		return err
	}
	p.isPaired = jp.IsPaired
	return nil
}
