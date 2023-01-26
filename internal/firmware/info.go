// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2022 Canonical Ltd
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

package firmware

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// SideInfo holds snap metadata that is crucial for the tracking of
// firmware and for the working of the system offline and for which
// the store is the canonical source.
//
// It can be marshalled and will be stored in the system state for
// each currently installed firmware revision so it needs to be evolved
// carefully.
type SideInfo struct {
	RealName    string              `yaml:"name,omitempty" json:"name,omitempty"`
	Revision    Revision            `yaml:"revision" json:"revision"`
}
