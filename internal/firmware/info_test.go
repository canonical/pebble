/*
 * Copyright (C) 2023 Canonical Ltd
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

package firmware_test

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internal/firmware"
)

func Test(t *testing.T) { TestingT(t) }

type S struct{}

var _ = Suite(&S{})

const metaInfoTestPath = "test/path/fwinfo/json"

func (s *S) TestFwInfoFileMissing(c *C) {
	path := filepath.Join(c.MkDir(), metaInfoTestPath)
	defer firmware.UpdateMetaInfoPath(path)()

	_, err := firmware.MetaInfoFromRunning()
	c.Assert(err, ErrorMatches, `cannot open firmware metadata file.*`)
}

func (s *S) TestFwInfoFileContentInvalid(c *C) {
	path := filepath.Join(c.MkDir(), metaInfoTestPath)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		c.Errorf("failed to create directory tree %s: %v", filepath.Dir(path), err)
	}
	if err := ioutil.WriteFile(path, []byte(``), 0755); err != nil {
		c.Errorf("failed to create file %s", path)
	}
	defer firmware.UpdateMetaInfoPath(path)()

	_, err := firmware.MetaInfoFromRunning()
	c.Assert(err, ErrorMatches, `cannot parse firmware metadata json.*`)
}

func (s *S) TestFwInfoFileValid(c *C) {
	path := filepath.Join(c.MkDir(), metaInfoTestPath)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		c.Errorf("failed to create directory tree %s: %v", filepath.Dir(path), err)
	}
	defer firmware.UpdateMetaInfoPath(path)()

	tests := []struct {
		json string
		info *firmware.Info
	}{
		{
			`{}`,
			&firmware.Info{},
		},
		{
			`{"name":"foo"}`,
			&firmware.Info{OriginalName: "foo"},
		},
		{
			`{"name":"foo", "version":"1.0~dev", "summary":"Foo firmware dev build"}`,
			&firmware.Info{OriginalName: "foo", Version: "1.0~dev", OriginalSummary: "Foo firmware dev build"},
		},
	}
	for _, t := range tests {
		if err := ioutil.WriteFile(path, []byte(t.json), 0755); err != nil {
			c.Errorf("failed to create file %s", path)
		}

		info, err := firmware.MetaInfoFromRunning()
		c.Assert(err, IsNil)
		c.Assert(info, DeepEquals, t.info)
		// At this point in time GetInfo is only a wrapper around metaInfoFromRunning, but
		// once more complexity is added (i.e. this breaks) GetInfo needs its own test.
		info, err = firmware.GetInfo()
		c.Assert(err, IsNil)
		c.Assert(info, DeepEquals, t.info)
	}
}

func (s *S) TestInfoMethods(c *C) {
	tests := []struct {
		info    firmware.Info
		rev     firmware.Revision
		name    string
		summary string
	}{
		{
			firmware.Info{},
			firmware.Revision{},
			"",
			"",
		},
		{
			firmware.Info{OriginalName: "foo", OriginalSummary: "Foo firmware dev build", StoreInfo: firmware.StoreInfo{Revision: firmware.Revision{-1}}},
			firmware.Revision{-1},
			"foo",
			"Foo firmware dev build",
		},
		{
			firmware.Info{
				OriginalName:    "foo",
				OriginalSummary: "Foo firmware dev build",
				StoreInfo: firmware.StoreInfo{
					Revision:        firmware.Revision{-2},
					ApprovedName:    "bar",
					ApprovedSummary: "Bar firmware dev build",
				},
			},
			firmware.Revision{-2},
			"bar",
			"Bar firmware dev build",
		},
	}
	for _, t := range tests {
		c.Assert(t.info.Rev(), DeepEquals, t.rev)
		c.Assert(t.info.Name(), DeepEquals, t.name)
		c.Assert(t.info.Summary(), DeepEquals, t.summary)
	}
}
