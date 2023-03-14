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

/*
 * Package firmware extracts information from the running system by
 * inspecting metadata files embedded in the root filesystem.
 */
package firmware

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
)

// Info provides information about firmware.
type Info struct {
	Version         string
	OriginalName    string
	OriginalSummary string

	StoreInfo
}

// Rev returns the revision of the firmware.
func (i *Info) Rev() Revision {
	return i.Revision
}

// Name returns the store approved name of the firmware, otherwise the
// firmware supplied name (if the store name is not available).
func (i *Info) Name() string {
	if i.ApprovedName != "" {
		return i.ApprovedName
	}
	return i.OriginalName
}

// Summary returns the store approved summary of the firmware, otherwise the
// firmware supplied summary (if the store summary is not available).
func (i *Info) Summary() string {
	if i.ApprovedSummary != "" {
		return i.ApprovedSummary
	}
	return i.OriginalSummary
}

// GetInfo returns a populated Info structure describing the running firmware.
func GetInfo() (*Info, error) {
	// Information from the firmware will later be supplemented by approved
	// metadata coming from the store.
	return metaInfoFromRunning()
}

// StoreInfo holds firmware metadata for which the store is the
// canonical source.
//
// It can be marshalled and will be stored in the system state for
// each installed firmware so it needs to be evolved carefully.
type StoreInfo struct {
	Revision        Revision `json:"revision"`
	ApprovedName    string   `json:"name,omitempty"`
	ApprovedSummary string   `json:"summary,omitempty"`
}

// metaInfoPath points to firmware metadata located in the
// rootfs of a running system.
var metaInfoPath string = "/termus/meta/termus.json"

// metaInfo represents firmware metadata created during the firmware
// build process.
type metaInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Summary string `json:"summary"`
}

// metaInfoFromRunning returns the firmware metadata of the running system
// exported as an Info struct.
func metaInfoFromRunning() (*Info, error) {
	data, err := ioutil.ReadFile(metaInfoPath)
	if err != nil {
		return nil, fmt.Errorf("cannot open firmware metadata file %s: %w", metaInfoPath, err)
	}

	var m metaInfo
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("cannot parse firmware metadata json: %w", err)
	}
	return &Info{
		Version:         m.Version,
		OriginalName:    m.Name,
		OriginalSummary: m.Summary,
	}, nil
}
