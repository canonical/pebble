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

// TODO(benhoyt) - should we limit the file size (or total size) for a read-files response,
//                 or just keep push bytes till the client rejects it?
// TODO(benhoyt) - should we limit the file size (or total size) for a write-files request,
//                 or just keep receiving bytes till the disk fills up?

package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"os/user"
	"path"
	"path/filepath"
	"strconv"
	"strings"
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

type fileResult struct {
	Path  string       `json:"path"`
	Error *errorResult `json:"error,omitempty"`
	f     *os.File
}

// Custom Response implementation to serve the multipart.
type readFilesResponse struct {
	paths []string
}

func (r readFilesResponse) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	// We open all the files first so we can detect not-found and
	// permission-denied errors for each file, as we send the metadata and
	// errors first.
	result := make([]fileResult, len(r.paths))
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
		result[i] = fileResult{
			Path:  path,
			Error: fileErrorToResult(err),
			f:     f,
		}
	}

	// Write HTTP status and headers.
	mw := multipart.NewWriter(w)
	header := w.Header()
	header.Set("Content-Type", mw.FormDataContentType())
	status := http.StatusOK
	if firstErr != nil {
		status = http.StatusBadRequest
	}
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
	respType := ResponseTypeSync
	if firstErr != nil {
		respType = ResponseTypeError
	}
	metadata := respJSON{
		Type:       respType,
		Status:     status,
		StatusText: http.StatusText(status),
		Result:     result,
	}
	err = encoder.Encode(metadata)
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

func fileErrorToResult(err error) *errorResult {
	if err == nil {
		return nil
	}
	var kind errorKind
	switch {
	case errors.Is(err, os.ErrPermission):
		kind = errorKindPermissionDenied
	case errors.Is(err, os.ErrNotExist):
		kind = errorKindNotFound
	default:
		kind = errorKindGenericFileError
	}
	return &errorResult{
		Kind:    kind,
		Message: err.Error(),
	}
}

func fileErrorResponse(kind errorKind, message string) Response {
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

type fileInfoResult struct {
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

func fileInfoToResult(fullPath string, info os.FileInfo) fileInfoResult {
	mode := info.Mode()
	var psize *int64
	if mode.IsRegular() {
		size := info.Size()
		psize = &size
	}
	result := fileInfoResult{
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
			return fileErrorResponse(errorKindPermissionDenied, err.Error())
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
	result := make([]fileInfoResult, len(matches))
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
	contentType := r.Header.Get("Content-Type")
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return statusBadRequest("invalid Content-Type: %q", contentType)
	}

	switch mediaType {
	case "multipart/form-data":
		return writeFiles(r.Body, params["boundary"])
	case "application/json":
		var payload struct {
			Action string            `json:"action"`
			Dirs   []makeDirsItem    `json:"dirs"`
			Paths  []removePathsItem `json:"paths"`
		}
		decoder := json.NewDecoder(r.Body)
		if err := decoder.Decode(&payload); err != nil {
			return statusBadRequest("cannot decode request body: %v", err)
		}
		switch payload.Action {
		case "make-dirs":
			return makeDirs(payload.Dirs)
		case "remove":
			return removePaths(payload.Paths)
		case "write":
			return statusBadRequest(`must use multipart with "write" action`)
		default:
			return statusBadRequest("invalid action: %q", payload.Action)
		}
	default:
		return statusBadRequest("invalid media type: %q", mediaType)
	}
}

type writeFilesItem struct {
	Path        string `json:"path"`
	MakeDirs    bool   `json:"make-dirs"`
	Permissions string `json:"permissions"`
	UserID      int    `json:"user-id"`
	User        string `json:"user"`
	GroupID     int    `json:"group-id"`
	Group       string `json:"group"`
}

func writeFiles(body io.Reader, boundary string) Response {
	// Read metadata part (field name "request").
	mr := multipart.NewReader(body, boundary)
	part, err := mr.NextPart()
	if err != nil {
		return statusBadRequest("cannot read request metadata: %v", err)
	}
	if part.FormName() != "request" {
		return statusBadRequest(`metadata field name must be "request", not %q`, part.FormName())
	}

	// Decode metadata about files to write.
	var payload struct {
		Action string           `json:"action"`
		Files  []writeFilesItem `json:"files"`
	}
	decoder := json.NewDecoder(part)
	if err := decoder.Decode(&payload); err != nil {
		return statusBadRequest("cannot decode request metadata: %v", err)
	}
	if payload.Action != "write" {
		return statusBadRequest(`multipart action must be "write", not %q`, payload.Action)
	}
	if len(payload.Files) == 0 {
		return statusBadRequest("must specify one or more files")
	}
	infos := make(map[string]writeFilesItem)
	for _, file := range payload.Files {
		infos[file.Path] = file
		if !path.IsAbs(file.Path) {
			return statusBadRequest("paths must be absolute (%q is not)", file.Path)
		}
		_, err = parsePermissions(file.Permissions, 0o644)
		if err != nil {
			return statusBadRequest(err.Error())
		}
	}

	errors := make(map[string]error)
	for i := 0; ; i++ {
		part, err = mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return statusBadRequest("cannot read file part %d: %v", i, err)
		}
		if !strings.HasPrefix(part.FormName(), "file:") {
			return statusBadRequest(`field name must be in format "file:p", not %q`, part.FormName())
		}
		p := part.FormName()[len("file:"):]
		info, ok := infos[p]
		if !ok {
			return statusBadRequest("no metadata for path %q", p)
		}
		errors[p] = writeFile(info, part)
		part.Close()
	}

	// Build list of results with any errors.
	result := make([]fileResult, len(payload.Files))
	var firstErr error
	for i, file := range payload.Files {
		err, ok := errors[file.Path]
		if !ok {
			// Ensure we wrote all the files in the metadata.
			err = fmt.Errorf("no file content for path %q", file.Path)
		}
		if err != nil && firstErr == nil {
			firstErr = err
		}
		result[i] = fileResult{
			Path:  file.Path,
			Error: fileErrorToResult(err),
		}
	}
	return respWithError(result, firstErr)
}

func writeFile(item writeFilesItem, source io.Reader) error {
	// Create parent directory if needed
	perm, err := parsePermissions(item.Permissions, 0o644)
	if err != nil {
		return err
	}
	if item.MakeDirs {
		err = os.MkdirAll(path.Dir(item.Path), perm)
		if err != nil {
			return fmt.Errorf("error creating directory: %w", err)
		}
	}

	// Create file and write contents.
	f, err := os.OpenFile(item.Path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return fmt.Errorf("error creating file: %w", err)
	}
	_, err = io.Copy(f, source)
	if err != nil {
		_ = f.Close()
		return fmt.Errorf("error writing file: %w", err)
	}
	err = f.Close()
	if err != nil {
		return fmt.Errorf("error closing file: %w", err)
	}

	// Update user and group if necessary.
	err = updateUserGroup(item.Path, item.UserID, item.User, item.GroupID, item.Group)
	if err != nil {
		return fmt.Errorf("error updating permissions: %w", err)
	}
	return nil
}

func parsePermissions(permissions string, defaultMode os.FileMode) (os.FileMode, error) {
	if permissions == "" {
		return defaultMode, nil
	}
	perm, err := strconv.ParseUint(permissions, 8, 32)
	if err != nil || len(permissions) != 3 {
		return 0, fmt.Errorf("permissions must be a 3-digit octal string, not %q", permissions)
	}
	return os.FileMode(perm), nil
}

type makeDirsItem struct {
	Path        string `json:"path"`
	MakeParents bool   `json:"make-parents"`
	Permissions string `json:"permissions"`
	UserID      int    `json:"user-id"`
	User        string `json:"user"`
	GroupID     int    `json:"group-id"`
	Group       string `json:"group"`
}

func makeDirs(dirs []makeDirsItem) Response {
	result := make([]fileResult, len(dirs))
	var firstErr error
	for i, dir := range dirs {
		err := makeDir(dir)
		if err != nil && firstErr == nil {
			firstErr = err
		}
		result[i] = fileResult{
			Path:  dir.Path,
			Error: fileErrorToResult(err),
		}
	}
	return respWithError(result, firstErr)
}

func makeDir(dir makeDirsItem) error {
	if !path.IsAbs(dir.Path) {
		return fmt.Errorf("paths must be absolute (%q is not)", dir.Path)
	}
	perm, err := parsePermissions(dir.Permissions, 0o775)
	if err != nil {
		return err
	}
	if dir.MakeParents {
		err = os.MkdirAll(dir.Path, perm)
	} else {
		err = os.Mkdir(dir.Path, perm)
	}
	if err != nil {
		return err
	}
	return updateUserGroup(dir.Path, dir.UserID, dir.User, dir.GroupID, dir.Group)
}

func updateUserGroup(path string, uid int, username string, gid int, group string) error {
	if uid == 0 && username == "" && gid == 0 && group == "" {
		return nil
	}
	if uid == 0 && username != "" {
		u, err := user.Lookup(username)
		if err != nil {
			return fmt.Errorf("error looking up user: %w", err)
		}
		uid, _ = strconv.Atoi(u.Uid)
	}
	if gid == 0 && group != "" {
		g, err := user.LookupGroup(group)
		if err != nil {
			return fmt.Errorf("error looking up group: %w", err)
		}
		gid, _ = strconv.Atoi(g.Gid)
	}
	if uid == 0 || gid == 0 {
		return fmt.Errorf("must set both user and group together")
	}
	err := os.Chown(path, uid, gid)
	if err != nil {
		return fmt.Errorf("error changing user and group: %w", err)
	}
	return nil
}

type removePathsItem struct {
	Path      string `json:"path"`
	Recursive bool   `json:"recursive"`
}

func removePaths(paths []removePathsItem) Response {
	result := make([]fileResult, len(paths))
	var firstErr error
	for i, p := range paths {
		err := removePath(p.Path, p.Recursive)
		if err != nil && firstErr == nil {
			firstErr = err
		}
		result[i] = fileResult{
			Path:  p.Path,
			Error: fileErrorToResult(err),
		}
	}
	return respWithError(result, firstErr)
}

func removePath(p string, recursive bool) error {
	if !path.IsAbs(p) {
		return fmt.Errorf("paths must be absolute (%q is not)", p)
	}
	if recursive {
		return os.RemoveAll(p)
	} else {
		return os.Remove(p)
	}
}

// TODO: move to response.go?
func respWithError(result interface{}, err error) Response {
	respType := ResponseTypeSync
	status := http.StatusOK
	if err != nil {
		respType = ResponseTypeError
		status = http.StatusBadRequest
	}
	return &resp{
		Type:   respType,
		Status: status,
		Result: result,
	}
}
