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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
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
		for _, p := range paths {
			if !path.IsAbs(p) {
				return statusBadRequest("paths must be absolute (%q is not)", p)
			}
		}
		return readFilesResponse{paths: paths}
	case "list":
		pattern := query.Get("pattern")
		if !path.IsAbs(pattern) {
			return statusBadRequest("pattern must be absolute (%q is not)", pattern)
		}
		directoryItself := query.Get("directory") == "true"
		return listFiles(pattern, directoryItself)
	default:
		return statusBadRequest("invalid action %q", action)
	}
}

type readFileResult struct {
	Path  string       `json:"path"`
	Error *errorResult `json:"error,omitempty"`
	f     *os.File
}

type readFilesResponse struct {
	paths []string
}

func (r readFilesResponse) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	// We open all the files first so we can detect not-found and
	// permission-denied errors for each file, as we send the metadata and
	// errors first.
	result := make([]readFileResult, len(r.paths))
	var firstErr error
	for i, path := range r.paths {
		f, err := os.Open(path)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
		} else {
			defer f.Close()
		}
		result[i] = readFileResult{
			Path:  path,
			Error: fileErrorToResult(err),
			f:     f,
		}
	}

	// Write HTTP status and headers.
	mw := multipart.NewWriter(w)
	header := w.Header()
	header.Set("Content-Type", mw.FormDataContentType())
	status := fileErrorToStatus(firstErr)
	w.WriteHeader(status)

	// Write first part: response metadata in JSON format.
	mh := textproto.MIMEHeader{}
	mh.Set("Content-Type", "application/json")
	mh.Set("Content-Disposition", `form-data; name="response"`)
	part, err := mw.CreatePart(mh)
	if err != nil {
		http.Error(w, "\n"+err.Error(), http.StatusInternalServerError)
		return
	}
	encoder := json.NewEncoder(part)
	encoder.SetIndent("", "    ") // TODO(benhoyt) - remove after testing
	respType := ResponseTypeSync
	if firstErr != nil {
		respType = ResponseTypeError
	}
	resp := respJSON{
		Type:       respType,
		Status:     status,
		StatusText: http.StatusText(status),
		Result:     result,
	}
	err = encoder.Encode(resp)
	if err != nil {
		http.Error(w, "\n"+err.Error(), http.StatusInternalServerError)
		return
	}

	// Write file content for each path.
	for _, file := range result {
		if file.Error != nil {
			continue
		}
		fw, err := mw.CreateFormFile("path:"+file.Path, path.Base(file.Path))
		if err != nil {
			http.Error(w, "\n"+err.Error(), http.StatusInternalServerError)
			return
		}
		_, err = io.Copy(fw, file.f)
		if err != nil {
			http.Error(w, "\n"+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	// Write the multipart trailer (last boundary marker).
	err = mw.Close()
	if err != nil {
		http.Error(w, "\n"+err.Error(), http.StatusInternalServerError)
		return
	}
}

func fileErrorToStatus(err error) int {
	if err != nil {
		return http.StatusBadRequest
	}
	return http.StatusOK
}

func fileErrorToResult(err error) *errorResult {
	switch {
	case errors.Is(err, os.ErrPermission):
		return &errorResult{
			Kind:    errorKindPermissionDenied,
			Message: err.Error(),
		}
	case errors.Is(err, os.ErrNotExist):
		return &errorResult{
			Kind:    errorKindNotFound,
			Message: err.Error(),
		}
	case err != nil:
		return &errorResult{
			Kind:    errorKindGenericFileError,
			Message: err.Error(),
		}
	default:
		return nil
	}
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

type fileResult struct {
	Path         string   `json:"path"`
	Name         string   `json:"name"`
	Type         fileType `json:"type"`
	Size         *int64   `json:"size,omitempty"`
	Permissions  string   `json:"permissions"`
	LastModified string   `json:"last-modified"`
}

type fileType string

const (
	fileTypeFile      fileType = "file"
	fileTypeDirectory fileType = "directory"
	fileTypeSymlink   fileType = "symlink"
	fileTypeSocket    fileType = "socket"
	fileTypeNamedPipe fileType = "named-pipe"
	fileTypeDevice    fileType = "device"
	fileTypeUnknown   fileType = "unknown"
)

func fileModeToType(mode os.FileMode) fileType {
	switch {
	case mode&os.ModeType == 0:
		return fileTypeFile
	case mode&os.ModeDir != 0:
		return fileTypeDirectory
	case mode&os.ModeSymlink != 0:
		return fileTypeSymlink
	case mode&os.ModeSocket != 0:
		return fileTypeSocket
	case mode&os.ModeNamedPipe != 0:
		return fileTypeNamedPipe
	case mode&os.ModeDevice != 0:
		return fileTypeDevice
	default:
		return fileTypeUnknown
	}
}

func fileInfoToResult(fullPath string, info os.FileInfo) fileResult {
	mode := info.Mode()
	var psize *int64
	if mode.IsRegular() {
		size := info.Size()
		psize = &size
	}
	result := fileResult{
		Path:         fullPath,
		Name:         path.Base(fullPath),
		Type:         fileModeToType(mode),
		Size:         psize,
		Permissions:  fmt.Sprintf("%03o", mode.Perm()),
		LastModified: info.ModTime().Format(time.RFC3339),
	}
	return result
}

func listFiles(pattern string, directoryItself bool) Response {
	st, err := os.Stat(pattern)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			// Pattern path exists but we don't have access.
			return errorResponse(errorKindPermissionDenied, err.Error())
		}
		if !errors.Is(err, os.ErrNotExist) {
			// Some other error (NotExist is okay).
			return statusBadRequest("cannot fetch path information: %v", err)
		}
	} else if st.IsDir() && !directoryItself {
		// If pattern is a directory, use "dir/*" as the glob.
		pattern = path.Join(pattern, "*")
	}

	// List files that match this glob pattern.
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return statusBadRequest("cannot list files: %v", err)
	}

	// Loop through files, get stat info, and convert to result.
	result := make([]fileResult, len(matches))
	for i, match := range matches {
		info, err := os.Lstat(match)
		if err != nil {
			return statusBadRequest("cannot fetch file information: %v", err)
		}
		result[i] = fileInfoToResult(match, info)
	}
	return SyncResponse(result)
}

func v1PostFiles(c *Command, r *http.Request, _ *userState) Response {
	return statusBadRequest("TODO: not yet implemented")
}
