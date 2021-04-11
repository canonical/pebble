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
	"io/ioutil"
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

	"github.com/canonical/pebble/internal/osutil"
)

func v1GetFiles(_ *Command, req *http.Request, _ *userState) Response {
	query := req.URL.Query()
	action := query.Get("action")
	switch action {
	case "read":
		paths := query["path"]
		if len(paths) == 0 {
			return statusBadRequest("must specify one or more paths")
		}
		return readFilesResponse{paths: paths}
	case "list":
		pattern := query.Get("pattern")
		if pattern == "" {
			return statusBadRequest("must specify pattern")
		}
		directory := query.Get("directory")
		if directory != "true" && directory != "false" && directory != "" {
			return statusBadRequest(`directory parameter must be "true" or "false"`)
		}
		return listFiles(pattern, directory == "true")
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

// Reading files

func (r readFilesResponse) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Header.Get("Accept") != "multipart/form-data" {
		errResp := statusBadRequest(`must accept multipart/form-data`)
		errResp.ServeHTTP(w, req)
		return
	}

	// We open all the files first so we can detect not-found and
	// permission-denied errors for each file, as we send the metadata and
	// errors first.
	result := make([]fileResult, len(r.paths))
	var firstErr error
	for i, p := range r.paths {
		f, err := openForRead(p)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
		} else {
			defer f.Close()
		}
		result[i] = fileResult{
			Path:  p,
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
	metadata := respJSON{
		Type:       ResponseTypeSync,
		Status:     status,
		StatusText: http.StatusText(status),
		Result:     result,
	}
	err = encoder.Encode(metadata)
	if err != nil {
		http.Error(w, "\n"+err.Error(), http.StatusInternalServerError)
		return
	}

	// Write file content for each p.
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

func nonAbsolutePathError(p string) error {
	return fmt.Errorf("paths must be absolute (%q is not)", p)
}

func openForRead(p string) (*os.File, error) {
	if !path.IsAbs(p) {
		return nil, nonAbsolutePathError(p)
	}
	// Pro-actively disallow reading directories (other errors will be caught
	// during read / io.Copy).
	if osutil.IsDir(p) {
		return nil, fmt.Errorf("cannot read a directory: %q", p)
	}
	return os.Open(p)
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

// Listing files

func listErrorResponse(err error) Response {
	status := http.StatusBadRequest
	kind := errorKindGenericFileError
	switch {
	case errors.Is(err, os.ErrNotExist):
		status = http.StatusNotFound
		kind = errorKindNotFound
	case errors.Is(err, os.ErrPermission):
		status = http.StatusForbidden
		kind = errorKindPermissionDenied
	}
	return &resp{
		Type: ResponseTypeError,
		Result: &errorResult{
			Message: err.Error(),
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
	if !path.IsAbs(pattern) {
		return statusBadRequest("pattern must be absolute (%q is not)", pattern)
	}
	result, err := listFilesErr(pattern, directoryItself)
	if err != nil {
		return listErrorResponse(err)
	}
	return SyncResponse(result)
}

func listFilesErr(pattern string, directoryItself bool) ([]fileInfoResult, error) {
	info, err := os.Stat(pattern)
	if errors.Is(err, os.ErrNotExist) {
		dir, base := filepath.Split(pattern)
		result, err := readDirFiltered(dir, base)
		if err != nil {
			return nil, err
		}
		if len(result) == 0 {
			// They specified a file or pattern and it doesn't exist.
			return nil, os.ErrNotExist
		}
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || directoryItself {
		// Info about a single file (or directory entry itself).
		result := []fileInfoResult{fileInfoToResult(pattern, info)}
		return result, nil
	}
	return readDirFiltered(pattern, "")
}

func readDirFiltered(dir, pattern string) ([]fileInfoResult, error) {
	infos, err := ioutil.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var result []fileInfoResult
	for _, info := range infos {
		name := info.Name()
		matched := true
		if pattern != "" {
			matched, err = filepath.Match(pattern, name)
		}
		if matched {
			p := filepath.Join(dir, name)
			result = append(result, fileInfoToResult(p, info))
		}
	}
	return result, nil
}

func v1PostFiles(_ *Command, req *http.Request, _ *userState) Response {
	contentType := req.Header.Get("Content-Type")
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return statusBadRequest("invalid Content-Type %q", contentType)
	}

	switch mediaType {
	case "multipart/form-data":
		boundary := params["boundary"]
		if boundary == "" {
			return statusBadRequest("invalid boundary %q", boundary)
		}
		return writeFiles(req.Body, boundary)
	case "application/json":
		var payload struct {
			Action string            `json:"action"`
			Dirs   []makeDirsItem    `json:"dirs"`
			Paths  []removePathsItem `json:"paths"`
		}
		decoder := json.NewDecoder(req.Body)
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
			return statusBadRequest("invalid action %q", payload.Action)
		}
	default:
		return statusBadRequest("invalid media type %q", mediaType)
	}
}

// Writing files

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
			return statusBadRequest(`field name must be in format "file:/path", not %q`, part.FormName())
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
	return syncResponseWithError(result, firstErr)
}

func writeFile(item writeFilesItem, source io.Reader) error {
	if !path.IsAbs(item.Path) {
		return nonAbsolutePathError(item.Path)
	}

	// Create parent directory if needed
	if item.MakeDirs {
		err := os.MkdirAll(path.Dir(item.Path), 0o755)
		if err != nil {
			return fmt.Errorf("creating directory: %w", err)
		}
	}

	// Create file and write contents to temporary file.
	perm, err := parsePermissions(item.Permissions, 0o644)
	if err != nil {
		return err
	}
	tempPath := item.Path + ".pebble-temp"
	f, err := os.OpenFile(tempPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return fmt.Errorf("creating file: %w", err)
	}
	_, err = io.Copy(f, source)
	if err != nil {
		_ = f.Close()
		_ = os.Remove(tempPath)
		return fmt.Errorf("writing file: %w", err)
	}
	err = f.Close()
	if err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("closing file: %w", err)
	}

	// Update user and group if necessary.
	err = updateUserAndGroup(tempPath, item.UserID, item.User, item.GroupID, item.Group)
	if err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("setting user and group: %w", err)
	}

	// Atomically move temporary file to final location.
	err = os.Rename(tempPath, item.Path)
	if err != nil {
		return fmt.Errorf("moving temporary file to path: %w", err)
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

// Creating directories

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
	return syncResponseWithError(result, firstErr)
}

func makeDir(dir makeDirsItem) error {
	if !path.IsAbs(dir.Path) {
		return nonAbsolutePathError(dir.Path)
	}
	perm, err := parsePermissions(dir.Permissions, 0o755)
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
	err = updateUserAndGroup(dir.Path, dir.UserID, dir.User, dir.GroupID, dir.Group)
	if err != nil {
		return fmt.Errorf("setting user and group: %w", err)
	}
	return nil
}

// Because it's hard to test os.Chown with running the tests as root.
var (
	chown       = os.Chown
	lookupUser  = user.Lookup
	lookupGroup = user.LookupGroup
)

func updateUserAndGroup(path string, uid int, username string, gid int, group string) error {
	if uid == 0 && username == "" && gid == 0 && group == "" {
		return nil
	}
	if uid == 0 && username != "" {
		u, err := lookupUser(username)
		if err != nil {
			return err
		}
		uid, _ = strconv.Atoi(u.Uid)
	}
	if gid == 0 && group != "" {
		g, err := lookupGroup(group)
		if err != nil {
			return err
		}
		gid, _ = strconv.Atoi(g.Gid)
	}
	if uid == 0 || gid == 0 {
		return fmt.Errorf("must set both user and group together")
	}
	err := chown(path, uid, gid)
	if err != nil {
		return err
	}
	return nil
}

// Removing paths

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
	return syncResponseWithError(result, firstErr)
}

func removePath(p string, recursive bool) error {
	if !path.IsAbs(p) {
		return nonAbsolutePathError(p)
	}
	if recursive {
		return os.RemoveAll(p)
	}
	return os.Remove(p)
}
