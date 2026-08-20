#!/bin/sh
# conform.sh - makes the osutil package (copied from
# github.com/canonical/snapd/osutil) conform to pebble coding standards:
#
#   1. Rewrites import paths:
#        github.com/snapcore/snapd/osutil    -> github.com/canonical/pebble/internals/osutil
#        github.com/snapcore/snapd/testutil  -> github.com/canonical/pebble/internals/testutil
#        github.com/snapcore/snapd/logger    -> github.com/canonical/pebble/internals/logger
#        github.com/snapcore/snapd/dirs      -> github.com/canonical/pebble/internals/osutil/internal/dirs
#        github.com/snapcore/snapd/strutil   -> github.com/canonical/x-go/strutil
#        github.com/snapcore/snapd/gadget/quantity -> github.com/canonical/pebble/internals/osutil/internal/quantity
#        github.com/snapcore/snapd/gadget/gadgettest -> github.com/canonical/pebble/internals/osutil/internal/gadgettest
#   2. Removes the "// -*- Mode: Go; indent-tabs-mode: t -*-" emacs mode line.
#   3. Converts the /* ... */ GPL license header block into // line comments.
#   4. Renames all MockXXXX identifiers (and their doc comments) to FakeXXXX,
#      and the generic testutil.Mock helper to testutil.Fake.
#   5. Renames the SNAPD_UNSAFE_IO environment variable to UNSAFE_IO.
#   6. Deletes keyboard/xkb.go and keyboard/xkb_test.go, which depend on
#      functionality that isn't being ported to pebble.
#   7. Renames disks/mockdisk.go and disks/mockdisk_test.go to
#      disks/fakedisk.go and disks/fakedisk_test.go.
#
# This script is idempotent and only touches *.go files under internals/osutil,
# ignoring internals/osutil/internal/* (vendored dependencies that are
# conformed separately) and this script itself.
#
# Usage: sh internals/osutil/conform.sh

set -e

cd "$(dirname "$0")"

# 6. Drop files that depend on functionality not being ported to pebble.
rm -f keyboard/xkb.go keyboard/xkb_test.go
rmdir keyboard 2>/dev/null || true

# 7. Rename mockdisk.go/mockdisk_test.go to match the MockXXXX -> FakeXXXX
#    renaming applied to their contents.
[ -f disks/mockdisk.go ] && mv disks/mockdisk.go disks/fakedisk.go
[ -f disks/mockdisk_test.go ] && mv disks/mockdisk_test.go disks/fakedisk_test.go

FILES=$(find . -name '*.go' -not -path './internal/*' -not -name 'conform.sh')

for f in $FILES; do
	# 1. Import path rewrites (also covers sub-packages, e.g.
	#    github.com/snapcore/snapd/osutil/disks).
	sed -i \
		-e 's#github\.com/snapcore/snapd/osutil#github.com/canonical/pebble/internals/osutil#g' \
		-e 's#github\.com/snapcore/snapd/testutil#github.com/canonical/pebble/internals/testutil#g' \
		-e 's#github\.com/snapcore/snapd/logger#github.com/canonical/pebble/internals/logger#g' \
		-e 's#github\.com/snapcore/snapd/dirs#github.com/canonical/pebble/internals/osutil/internal/dirs#g' \
		-e 's#github\.com/snapcore/snapd/strutil#github.com/canonical/x-go/strutil#g' \
		-e 's#github\.com/snapcore/snapd/gadget/quantity#github.com/canonical/pebble/internals/osutil/internal/quantity#g' \
		-e 's#github\.com/snapcore/snapd/gadget/gadgettest#github.com/canonical/pebble/internals/osutil/internal/gadgettest#g' \
		"$f"

	# 2. Drop the emacs mode line.
	sed -i '/^\/\/ -\*- Mode: Go; indent-tabs-mode: t -\*-$/d' "$f"

	# 4. Rename MockXXXX -> FakeXXXX (identifiers and doc comments alike), and
	#    the generic testutil.Mock helper to testutil.Fake.
	sed -i -E 's/\bMock([A-Z][A-Za-z0-9_]*)/Fake\1/g; s/\btestutil\.Mock\b/testutil.Fake/g' "$f"

	# 5. Rename the SNAPD_UNSAFE_IO environment variable to UNSAFE_IO.
	sed -i 's/\bSNAPD_UNSAFE_IO\b/UNSAFE_IO/g' "$f"

	# Drop a leading blank line left behind by the mode-line removal above,
	# in files without a //go:build line before the license header.
	sed -i '1{/^$/d}' "$f"
done

# 3. Convert the /* ... */ Canonical GPL license header into // line comments.
#
# Only files with the exact snapd/pebble GPL boilerplate are affected; files
# with other headers (e.g. vendored code with an Apache or BSD license) are
# left untouched.
for f in $FILES; do
	python3 - "$f" <<'PYEOF'
import re
import sys

path = sys.argv[1]
with open(path) as fh:
	content = fh.read()

# Some files omit the leading " * " on otherwise-blank separator lines
# within the header, so those separators are matched leniently.
sep = r"(?: \*)?\n"

pattern = re.compile(
	r"/\*\n"
	r"( \* Copyright \(C\) [^\n]*\n)"
	+ sep +
	r" \* This program is free software: you can redistribute it and/or modify\n"
	r" \* it under the terms of the GNU General Public License version 3 as\n"
	r" \* published by the Free Software Foundation\.\n"
	+ sep +
	r" \* This program is distributed in the hope that it will be useful,\n"
	r" \* but WITHOUT ANY WARRANTY; without even the implied warranty of\n"
	r" \* MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE\.  See the\n"
	r" \* GNU General Public License for more details\.\n"
	+ sep +
	r" \* You should have received a copy of the GNU General Public License\n"
	r" \* along with this program\.  If not, see <http://www\.gnu\.org/licenses/>\.\n"
	+ sep +
	r" \*/\n"
)


def replace(match):
	copyright_line = match.group(1).strip()[2:]  # drop leading "* "
	lines = [
		"// " + copyright_line,
		"//",
		"// This program is free software: you can redistribute it and/or modify",
		"// it under the terms of the GNU General Public License version 3 as",
		"// published by the Free Software Foundation.",
		"//",
		"// This program is distributed in the hope that it will be useful,",
		"// but WITHOUT ANY WARRANTY; without even the implied warranty of",
		"// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the",
		"// GNU General Public License for more details.",
		"//",
		"// You should have received a copy of the GNU General Public License",
		"// along with this program.  If not, see <http://www.gnu.org/licenses/>.",
	]
	return "\n".join(lines) + "\n"


new_content = pattern.sub(replace, content)
if new_content != content:
	with open(path, "w") as fh:
		fh.write(new_content)
PYEOF
done

echo "Done."
