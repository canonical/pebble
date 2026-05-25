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

package timeutil_test

import (
	"testing"
	"time"

	"github.com/canonical/tc"

	"github.com/canonical/pebble/internals/timeutil"
)

type humanSuite struct {
	beforeDSTbegins, afterDSTbegins, beforeDSTends, afterDSTends time.Time
}

func TestHumanSuite(t *testing.T) {
	tc.Run(t, &humanSuite{})
}

func (s *humanSuite) SetUpSuite(c *tc.C) {
	loc, err := time.LoadLocation("Europe/London")
	c.Assert(err, tc.ErrorIsNil)

	s.beforeDSTbegins = time.Date(2017, 3, 26, 0, 59, 0, 0, loc)
	// note this is actually 2:01am DST
	s.afterDSTbegins = time.Date(2017, 3, 26, 1, 1, 0, 0, loc)

	// apparently no way to straight out initialise a time inside the DST overlap
	s.beforeDSTends = time.Date(2017, 10, 29, 0, 59, 0, 0, loc).Add(60 * time.Minute)
	s.afterDSTends = time.Date(2017, 10, 29, 1, 1, 0, 0, loc)

	// sanity check
	c.Check(s.beforeDSTbegins.Format("MST"), tc.Equals, s.afterDSTends.Format("MST"))
	c.Check(s.beforeDSTbegins.Format("MST"), tc.Equals, "GMT")
	c.Check(s.afterDSTbegins.Format("MST"), tc.Equals, s.beforeDSTends.Format("MST"))
	c.Check(s.afterDSTbegins.Format("MST"), tc.Equals, "BST")

	// “The month, day, hour, min, sec, and nsec values may be outside their
	//  usual ranges and will be normalized during the conversion.”
	// so you can always add or subtract 1 from a day and it'll just work \o/
	c.Check(time.Date(2017, -1, -1, -1, -1, -1, 0, loc), tc.DeepEquals, time.Date(2016, 10, 29, 22, 58, 59, 0, loc))
	c.Check(time.Date(2017, 13, 32, 25, 61, 63, 0, loc), tc.DeepEquals, time.Date(2018, 2, 2, 2, 2, 3, 0, loc))
}

func (s *humanSuite) TestHumanTimeDST(c *tc.C) {
	c.Check(timeutil.HumanTimeSince(s.beforeDSTbegins, s.afterDSTbegins, 300), tc.Equals, "today at 00:59 GMT")
	c.Check(timeutil.HumanTimeSince(s.beforeDSTends, s.afterDSTends, 300), tc.Equals, "today at 01:59 BST")
	c.Check(timeutil.HumanTimeSince(s.beforeDSTbegins, s.afterDSTends, 300), tc.Equals, "217 days ago, at 00:59 GMT")
}

func (s *humanSuite) TestHumanTimeDSTMore(c *tc.C) {
	loc, err := time.LoadLocation("Europe/London")
	c.Assert(err, tc.ErrorIsNil)
	d0 := time.Date(2018, 3, 23, 13, 14, 15, 0, loc)
	df := time.Date(2018, 3, 25, 13, 14, 15, 0, loc)
	c.Check(timeutil.HumanTimeSince(d0, df, 300), tc.Equals, "2 days ago, at 13:14 GMT")
	c.Check(timeutil.HumanTimeSince(df, d0, 300), tc.Equals, "in 2 days, at 13:14 BST")
}

func (*humanSuite) TestHuman(c *tc.C) {
	now := time.Now()

	for i, expected := range []string{
		"2 days ago, at ", "yesterday at ", "today at ", "tomorrow at ", "in 2 days, at ",
	} {
		t := now.AddDate(0, 0, i-2)
		timePart := t.Format("15:04 MST")
		c.Check(timeutil.Human(t), tc.Equals, expected+timePart)
	}

	// two outside of the 60-day cutoff:
	d1 := now.AddDate(0, -3, 0)
	d2 := now.AddDate(0, 3, 0)
	c.Check(timeutil.Human(d1), tc.Equals, d1.Format("2006-01-02"))
	c.Check(timeutil.Human(d2), tc.Equals, d2.Format("2006-01-02"))

}
