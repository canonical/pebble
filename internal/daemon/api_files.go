// Copyright (c) 2021 Canonical Ltd
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

package daemon

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"time"
)

func v1GetFiles(c *Command, r *http.Request, _ *userState) Response {
	query := r.URL.Query()
	action := query.Get("action")
	switch action {
	case "read":
		paths := query["path"]
		if len(paths) == 0 {
			return statusBadRequest("must specify one or more paths")
		}
		return getFiles(paths)
	case "list":
		return listFiles(query.Get("pattern"))
	default:
		return statusBadRequest("invalid action %q", action)
	}
}

func getFiles(paths []string) Response {
	return nil
}

func errorResponse(kind errorKind, message string) Response {
	status := 400
	switch kind {
	case errorKindNotFound:
		status = 404
	case errorKindPermissionDenied:
		status = 403
	}
	return &resp{
		Type: ResponseTypeError,
		Result: &errorResult{
			Message: message,
			Kind:    kind,
		},
		Status: status,
	}
}

// TODO: should we include "." and ".."?
func listFiles(pattern string) Response {
	st, err := os.Stat(pattern)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			return errorResponse(errorKindPermissionDenied, err.Error())
		}
		if !errors.Is(err, os.ErrNotExist) {
			return statusBadRequest("fetching path information: %v", err)
		}
	} else if st.IsDir() {
		pattern = path.Join(pattern, "*")
	}
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return statusBadRequest("listing files: %v", err)
	}
	type listFilesResult struct {
		Path         string `json:"path"`
		Name         string `json:"name"`
		Type         string `json:"type"`
		Size         *int64 `json:"size,omitempty"`
		Permissions  string `json:"permissions"`
		LastModified string `json:"last-modified"`
	}
	var result []listFilesResult
	for _, match := range matches {
		st, err := os.Lstat(match)
		if err != nil {
			return statusBadRequest("fetching file information: %v", err)
		}
		fileType := "file"
		var psize *int64
		if st.IsDir() {
			fileType = "directory"
			psize = nil
		} else {
			size := st.Size()
			psize = &size
		}
		r := listFilesResult{
			Path:         match,
			Name:         path.Base(match),
			Type:         fileType,
			Size:         psize,
			Permissions:  fmt.Sprintf("%03o", st.Mode().Perm()),
			LastModified: st.ModTime().Format(time.RFC3339), // TODO: format?
		}
		result = append(result, r)
	}
	return SyncResponse(result)
}

func v1PostFiles(c *Command, r *http.Request, _ *userState) Response {
	return statusBadRequest("TODO: not yet implemented")
}
