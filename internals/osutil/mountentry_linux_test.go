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

package osutil_test

import (
	"syscall"
	"testing"

	"github.com/canonical/tc"

	"github.com/canonical/pebble/internals/osutil"
)

type entrySuite struct{}

func TestEntrySuite(t *testing.T) {
	tc.Run(t, &entrySuite{})
}

func (s *entrySuite) TestString(c *tc.C) {
	ent0 := osutil.MountEntry{}
	c.Assert(ent0.String(), tc.Equals, "none none none defaults 0 0")
	ent1 := osutil.MountEntry{
		Name:    "/var/snap/foo/common",
		Dir:     "/var/snap/bar/common",
		Options: []string{"bind"},
	}
	c.Assert(ent1.String(), tc.Equals,
		"/var/snap/foo/common /var/snap/bar/common none bind 0 0")
	ent2 := osutil.MountEntry{
		Name:    "/dev/sda5",
		Dir:     "/media/foo",
		Type:    "ext4",
		Options: []string{"rw,noatime"},
	}
	c.Assert(ent2.String(), tc.Equals, "/dev/sda5 /media/foo ext4 rw,noatime 0 0")
	ent3 := osutil.MountEntry{
		Name:    "/dev/sda5",
		Dir:     "/media/My Files",
		Type:    "ext4",
		Options: []string{"rw,noatime"},
	}
	c.Assert(ent3.String(), tc.Equals, `/dev/sda5 /media/My\040Files ext4 rw,noatime 0 0`)
}

func (s *entrySuite) TestEqual(c *tc.C) {
	var a, b *osutil.MountEntry
	a = &osutil.MountEntry{}
	b = &osutil.MountEntry{}
	c.Assert(a.Equal(b), tc.Equals, true)
	a = &osutil.MountEntry{Dir: "foo"}
	b = &osutil.MountEntry{Dir: "foo"}
	c.Assert(a.Equal(b), tc.Equals, true)
	a = &osutil.MountEntry{Options: []string{"ro"}}
	b = &osutil.MountEntry{Options: []string{"ro"}}
	c.Assert(a.Equal(b), tc.Equals, true)
	a = &osutil.MountEntry{Dir: "foo"}
	b = &osutil.MountEntry{Dir: "bar"}
	c.Assert(a.Equal(b), tc.Equals, false)
	a = &osutil.MountEntry{}
	b = &osutil.MountEntry{Options: []string{"ro"}}
	c.Assert(a.Equal(b), tc.Equals, false)
	a = &osutil.MountEntry{Options: []string{"ro"}}
	b = &osutil.MountEntry{Options: []string{"rw"}}
	c.Assert(a.Equal(b), tc.Equals, false)
}

// Test that typical fstab entry is parsed correctly.
func (s *entrySuite) TestParseMountEntry1(c *tc.C) {
	e, err := osutil.ParseMountEntry("UUID=394f32c0-1f94-4005-9717-f9ab4a4b570b /               ext4    errors=remount-ro 0       1")
	c.Assert(err, tc.IsNil)
	c.Assert(e.Name, tc.Equals, "UUID=394f32c0-1f94-4005-9717-f9ab4a4b570b")
	c.Assert(e.Dir, tc.Equals, "/")
	c.Assert(e.Type, tc.Equals, "ext4")
	c.Assert(e.Options, tc.DeepEquals, []string{"errors=remount-ro"})
	c.Assert(e.DumpFrequency, tc.Equals, 0)
	c.Assert(e.CheckPassNumber, tc.Equals, 1)

	e, err = osutil.ParseMountEntry("none /tmp tmpfs")
	c.Assert(err, tc.IsNil)
	c.Assert(e.Name, tc.Equals, "none")
	c.Assert(e.Dir, tc.Equals, "/tmp")
	c.Assert(e.Type, tc.Equals, "tmpfs")
	c.Assert(e.Options, tc.IsNil)
	c.Assert(e.DumpFrequency, tc.Equals, 0)
	c.Assert(e.CheckPassNumber, tc.Equals, 0)
}

// Test that hash inside a field value is supported.
func (s *entrySuite) TestHashInFieldValue(c *tc.C) {
	e, err := osutil.ParseMountEntry("mhddfs#/mnt/dir1,/mnt/dir2 /mnt/dir fuse defaults,allow_other 0 0")
	c.Assert(err, tc.IsNil)
	c.Assert(e.Name, tc.Equals, "mhddfs#/mnt/dir1,/mnt/dir2")
	c.Assert(e.Dir, tc.Equals, "/mnt/dir")
	c.Assert(e.Type, tc.Equals, "fuse")
	c.Assert(e.Options, tc.DeepEquals, []string{"defaults", "allow_other"})
	c.Assert(e.DumpFrequency, tc.Equals, 0)
	c.Assert(e.CheckPassNumber, tc.Equals, 0)
}

// Test that options are parsed correctly
func (s *entrySuite) TestParseMountEntry2(c *tc.C) {
	e, err := osutil.ParseMountEntry("name dir type options,comma,separated 0 0")
	c.Assert(err, tc.IsNil)
	c.Assert(e.Name, tc.Equals, "name")
	c.Assert(e.Dir, tc.Equals, "dir")
	c.Assert(e.Type, tc.Equals, "type")
	c.Assert(e.Options, tc.DeepEquals, []string{"options", "comma", "separated"})
	c.Assert(e.DumpFrequency, tc.Equals, 0)
	c.Assert(e.CheckPassNumber, tc.Equals, 0)
}

// Test that whitespace escape codes are honored
func (s *entrySuite) TestParseMountEntry3(c *tc.C) {
	e, err := osutil.ParseMountEntry(`na\040me d\011ir ty\012pe optio\134ns 0 0`)
	c.Assert(err, tc.IsNil)
	c.Assert(e.Name, tc.Equals, "na me")
	c.Assert(e.Dir, tc.Equals, "d\tir")
	c.Assert(e.Type, tc.Equals, "ty\npe")
	c.Assert(e.Options, tc.DeepEquals, []string{`optio\ns`})
	c.Assert(e.DumpFrequency, tc.Equals, 0)
	c.Assert(e.CheckPassNumber, tc.Equals, 0)
}

// Test that number of fields is checked
func (s *entrySuite) TestParseMountEntry4(c *tc.C) {
	for _, s := range []string{
		"", "1", "1 2" /* skip 3, 4, 5 and 6 fields (valid case) */, "1 2 3 4 5 6 7",
	} {
		_, err := osutil.ParseMountEntry(s)
		c.Assert(err, tc.ErrorMatches, "expected between 3 and 6 fields, found [01237]")
	}
}

// Test that integers are parsed and error checked
func (s *entrySuite) TestParseMountEntry5(c *tc.C) {
	_, err := osutil.ParseMountEntry("name dir type options foo 0")
	c.Assert(err, tc.ErrorMatches, "cannot parse dump frequency: .*")
	_, err = osutil.ParseMountEntry("name dir type options 0 foo")
	c.Assert(err, tc.ErrorMatches, "cannot parse check pass number: .*")
}

// Test that last two integer fields default to zero if not present.
func (s *entrySuite) TestParseMountEntry6(c *tc.C) {
	e, err := osutil.ParseMountEntry("name dir type options")
	c.Assert(err, tc.IsNil)
	c.Assert(e.DumpFrequency, tc.Equals, 0)
	c.Assert(e.CheckPassNumber, tc.Equals, 0)

	e, err = osutil.ParseMountEntry("name dir type options 5")
	c.Assert(err, tc.IsNil)
	c.Assert(e.DumpFrequency, tc.Equals, 5)
	c.Assert(e.CheckPassNumber, tc.Equals, 0)

	e, err = osutil.ParseMountEntry("name dir type options 5 7")
	c.Assert(err, tc.IsNil)
	c.Assert(e.DumpFrequency, tc.Equals, 5)
	c.Assert(e.CheckPassNumber, tc.Equals, 7)
}

// Test (string) options -> (int) flag conversion code.
func (s *entrySuite) TestMountOptsToFlags(c *tc.C) {
	flags, err := osutil.MountOptsToFlags(nil)
	c.Assert(err, tc.IsNil)
	c.Assert(flags, tc.Equals, 0)
	flags, err = osutil.MountOptsToFlags([]string{"ro", "nodev", "nosuid"})
	c.Assert(err, tc.IsNil)
	c.Assert(flags, tc.Equals, syscall.MS_RDONLY|syscall.MS_NODEV|syscall.MS_NOSUID)
	_, err = osutil.MountOptsToFlags([]string{"bogus"})
	c.Assert(err, tc.ErrorMatches, `unsupported mount option: "bogus"`)
	// The x-snapd-prefix is reserved for non-kernel parameters that do not
	// translate to kernel level mount flags. This is similar to systemd or
	// udisks that use fstab options to convey additional data.
	flags, err = osutil.MountOptsToFlags([]string{"x-snapd.foo"})
	c.Assert(err, tc.IsNil)
	c.Assert(flags, tc.Equals, 0)
}

// Test (string) options -> (int, unparsed) flag conversion code.
func (s *entrySuite) TestMountOptsToCommonFlags(c *tc.C) {
	flags, unparsed := osutil.MountOptsToCommonFlags(nil)
	c.Assert(flags, tc.Equals, 0)
	c.Assert(unparsed, tc.HasLen, 0)
	flags, unparsed = osutil.MountOptsToCommonFlags([]string{"ro", "nodev", "nosuid"})
	c.Assert(flags, tc.Equals, syscall.MS_RDONLY|syscall.MS_NODEV|syscall.MS_NOSUID)
	c.Assert(unparsed, tc.HasLen, 0)
	flags, unparsed = osutil.MountOptsToCommonFlags([]string{"bogus"})
	c.Assert(flags, tc.Equals, 0)
	c.Assert(unparsed, tc.DeepEquals, []string{"bogus"})
	// The x-snapd-prefix is reserved for non-kernel parameters that do not
	// translate to kernel level mount flags. This is similar to systemd or
	// udisks that use fstab options to convey additional data. Those are not
	// returned as "unparsed" as we don't want to pass them to the kernel.
	flags, unparsed = osutil.MountOptsToCommonFlags([]string{"x-snapd.foo"})
	c.Assert(flags, tc.Equals, 0)
	c.Assert(unparsed, tc.HasLen, 0)
}

func (s *entrySuite) TestOptStr(c *tc.C) {
	e := &osutil.MountEntry{Options: []string{"key=value"}}
	val, ok := e.OptStr("key")
	c.Assert(ok, tc.Equals, true)
	c.Assert(val, tc.Equals, "value")

	val, ok = e.OptStr("missing")
	c.Assert(ok, tc.Equals, false)
	c.Assert(val, tc.Equals, "")
}

func (s *entrySuite) TestOptBool(c *tc.C) {
	e := &osutil.MountEntry{Options: []string{"key"}}
	val := e.OptBool("key")
	c.Assert(val, tc.Equals, true)

	val = e.OptBool("missing")
	c.Assert(val, tc.Equals, false)
}
