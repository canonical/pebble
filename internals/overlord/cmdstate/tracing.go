// Copyright (c) 2026 Canonical Ltd
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

package cmdstate

// Span attribute names used for exec tasks. The full attribute key is
// prefixed with the program name (see tracing.AttrKey). Standard process
// attributes (process.pid and so on) are used where they exist.
const (
	attrExecTerminal    = "exec.terminal"
	attrExecInteractive = "exec.interactive"
	attrExecTimeout     = "exec.timeout"
	attrExecTimedOut    = "exec.timed-out"
	attrExecSignal      = "exec.signal"
	attrError           = "error.message"
)
