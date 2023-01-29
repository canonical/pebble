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

package main

import (
	"github.com/canonical/pebble/client"
)

var RunMain = run

var ClientConfig = &clientConfig

func Client() *client.Client {
	cli, err := client.New(ClientConfig)
	if err != nil {
		panic("cannot create client:" + err.Error())
	}
	return cli
}

var (
	CanUnicode      = canUnicode
	ColorTable      = colorTable
	MonoColorTable  = mono
	ColorColorTable = color
	NoEscColorTable = noesc

	WriteWarningTimestamp = writeWarningTimestamp
	MaybePresentWarnings  = maybePresentWarnings

	GetEnvPaths = getEnvPaths
)

func FakeIsStdoutTTY(t bool) (restore func()) {
	oldIsStdoutTTY := isStdoutTTY
	isStdoutTTY = t
	return func() {
		isStdoutTTY = oldIsStdoutTTY
	}
}

func FakeIsStdinTTY(t bool) (restore func()) {
	oldIsStdinTTY := isStdinTTY
	isStdinTTY = t
	return func() {
		isStdinTTY = oldIsStdinTTY
	}
}

func PebbleMain() (exitCode int) {
	oldOsExit := osExit
	osExit = func(code int) {
		panic(&exitStatus{code})
	}
	defer func() {
		osExit = oldOsExit
		if v := recover(); v != nil {
			if e, ok := v.(*exitStatus); ok {
				exitCode = e.code
			} else {
				panic(v)
			}
		}
	}()
	main()
	return
}
