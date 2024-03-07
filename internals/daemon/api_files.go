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
	pathpkg "path"
	"strconv"
	"syscall"
	"time"

	"github.com/canonical/pebble/internals/osutil"
	"github.com/canonical/pebble/internals/osutil/sys"
)

const minBoundaryLength = 32

func v1GetFiles(_ *Command, req *http.Request, _ *UserState) Response {
	query := req.URL.Query()
	action := query.Get("action")
	switch action {
	case "read":
		paths := query["path"]
		if len(paths) == 0 {
			return BadRequest("must specify one or more paths")
		}
		if req.Header.Get("Accept") != "multipart/form-data" {
			return BadRequest(`must accept multipart/form-data`)
		}
		return readFilesResponse{paths: paths}
	case "list":
		path := query.Get("path")
		if path == "" {
			return BadRequest("must specify path")
		}
		pattern := query.Get("pattern")
		itself := query.Get("itself")
		if itself != "true" && itself != "false" && itself != "" {
			return BadRequest(`itself parameter must be "true" or "false"`)
		}
		return listFilesResponse(path, pattern, itself == "true")
	default:
		return BadRequest("invalid action %q", action)
	}
}

type fileResult struct {
	Path  string       `json:"path"`
	Error *errorResult `json:"error,omitempty"`
}

// Reading files

// Custom Response implementation to serve the multipart.
type readFilesResponse struct {
	paths []string
}

func (r readFilesResponse) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	// Write HTTP status and headers. HTTP status is always OK because we don't
	// know any better until we've read all the files.
	mw := multipart.NewWriter(w)
	header := w.Header()
	header.Set("Content-Type", mw.FormDataContentType())
	w.WriteHeader(http.StatusOK)

	// Read each file's contents to multipart response.
	result := make([]fileResult, len(r.paths))
	for i, path := range r.paths {
		err := readFile(path, mw)
		result[i] = fileResult{
			Path:  path,
			Error: fileErrorToResult(err),
		}
	}

	// At the end, write response metadata in JSON format.
	mh := textproto.MIMEHeader{}
	mh.Set("Content-Type", "application/json")
	mh.Set("Content-Disposition", `form-data; name="response"`)
	part, err := mw.CreatePart(mh)
	if err != nil {
		// Can't write metadata -- writing the error message on a new line is
		// about the best we can do.
		fmt.Fprint(w, "\n", err)
		return
	}
	encoder := json.NewEncoder(part)
	metadata := respJSON{
		Type:       ResponseTypeSync,
		Status:     http.StatusOK,
		StatusText: http.StatusText(http.StatusOK),
		Result:     result,
	}
	err = encoder.Encode(metadata)
	if err != nil {
		fmt.Fprint(w, "\n", err)
		return
	}

	// Write the multipart trailer (last boundary marker).
	err = mw.Close()
	if err != nil {
		fmt.Fprint(w, "\n", err)
		return
	}
}

func nonAbsolutePathError(path string) error {
	return fmt.Errorf("paths must be absolute, got %q", path)
}

func readFile(path string, mw *multipart.Writer) error {
	if !pathpkg.IsAbs(path) {
		return nonAbsolutePathError(path)
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("can only read a regular file: %q", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	fw, err := mw.CreateFormFile("files", path)
	if err != nil {
		return err
	}
	_, err = io.Copy(fw, f)
	if err != nil {
		return err
	}
	return nil
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

func fileErrorToStatus(err error) int {
	switch {
	case err == nil:
		return http.StatusOK
	case errors.Is(err, os.ErrNotExist):
		return http.StatusNotFound
	case errors.Is(err, os.ErrPermission):
		return http.StatusForbidden
	default:
		return http.StatusBadRequest
	}
}

type fileInfoResult struct {
	Path         string   `json:"path"`
	Name         string   `json:"name"`
	Type         fileType `json:"type"`
	Size         *int64   `json:"size,omitempty"`
	Permissions  string   `json:"permissions"`
	LastModified string   `json:"last-modified"`
	UserID       *int     `json:"user-id"`
	User         string   `json:"user"`
	GroupID      *int     `json:"group-id"`
	Group        string   `json:"group"`
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

func fileInfoToResult(fullPath string, info os.FileInfo, userCache, groupCache map[int]string) fileInfoResult {
	mode := info.Mode()
	var psize *int64
	if mode.IsRegular() {
		size := info.Size()
		psize = &size
	}
	result := fileInfoResult{
		Path:         fullPath,
		Name:         pathpkg.Base(fullPath),
		Type:         fileModeToType(mode),
		Size:         psize,
		Permissions:  fmt.Sprintf("%03o", mode.Perm()),
		LastModified: info.ModTime().Format(time.RFC3339),
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		uidInt := int(stat.Uid)
		gidInt := int(stat.Gid)
		result.UserID = &uidInt
		result.GroupID = &gidInt

		// Look up user and group names (cache per API call for efficiency).
		result.User = userCache[uidInt]
		if result.User == "" {
			u, err := user.LookupId(strconv.Itoa(uidInt))
			if err == nil {
				result.User = u.Username
				userCache[uidInt] = u.Username
			}
		}

		result.Group = groupCache[gidInt]
		if result.Group == "" {
			g, err := user.LookupGroupId(strconv.Itoa(gidInt))
			if err == nil {
				result.Group = g.Name
				groupCache[gidInt] = g.Name
			}
		}
	}
	return result
}

func listFilesResponse(path, pattern string, itself bool) Response {
	if !pathpkg.IsAbs(path) {
		return BadRequest("path must be absolute, got %q", path)
	}
	result, err := listFiles(path, pattern, itself)
	if err != nil {
		return &resp{
			Type:   ResponseTypeError,
			Result: fileErrorToResult(err),
			Status: fileErrorToStatus(err),
		}
	}
	return SyncResponse(result)
}

func listFiles(path, pattern string, itself bool) ([]fileInfoResult, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	var infos []os.FileInfo
	var dir string
	if !info.IsDir() || itself {
		// Info about a single file (or directory entry itself).
		infos = []os.FileInfo{info}
		dir = pathpkg.Dir(path)
	} else {
		// List an entire directory.
		infos, err = ioutil.ReadDir(path)
		if err != nil {
			return nil, err
		}
		dir = path
	}

	result := make([]fileInfoResult, 0) // want "no results" to be [], not nil
	userCache := make(map[int]string)
	groupCache := make(map[int]string)
	for _, info = range infos {
		name := info.Name()
		matched := true
		if pattern != "" {
			matched, err = pathpkg.Match(pattern, name)
			if err != nil {
				return nil, err
			}
		}
		if matched {
			fullPath := pathpkg.Join(dir, name)
			result = append(result, fileInfoToResult(fullPath, info, userCache, groupCache))
		}
	}
	return result, nil
}

func v1PostFiles(_ *Command, req *http.Request, _ *UserState) Response {
	contentType := req.Header.Get("Content-Type")
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return BadRequest("invalid Content-Type %q", contentType)
	}

	switch mediaType {
	case "multipart/form-data":
		boundary := params["boundary"]
		if len(boundary) < minBoundaryLength {
			return BadRequest("invalid boundary %q", boundary)
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
			return BadRequest("cannot decode request body: %v", err)
		}
		switch payload.Action {
		case "make-dirs":
			return makeDirs(payload.Dirs)
		case "remove":
			return removePaths(payload.Paths)
		case "write":
			return BadRequest(`must use multipart with "write" action`)
		default:
			return BadRequest("invalid action %q", payload.Action)
		}
	default:
		return BadRequest("invalid media type %q", mediaType)
	}
}

// Writing files

type writeFilesItem struct {
	Path        string `json:"path"`
	MakeDirs    bool   `json:"make-dirs"`
	Permissions string `json:"permissions"`
	UserID      *int   `json:"user-id"`
	User        string `json:"user"`
	GroupID     *int   `json:"group-id"`
	Group       string `json:"group"`
}

func writeFiles(body io.Reader, boundary string) Response {
	// Read metadata part (field name "request").
	mr := multipart.NewReader(body, boundary)
	part, err := mr.NextPart()
	if err != nil {
		return BadRequest("cannot read request metadata: %v", err)
	}
	if part.FormName() != "request" {
		return BadRequest(`metadata field name must be "request", got %q`, part.FormName())
	}

	// Decode metadata about files to write.
	var payload struct {
		Action string           `json:"action"`
		Files  []writeFilesItem `json:"files"`
	}
	decoder := json.NewDecoder(part)
	if err := decoder.Decode(&payload); err != nil {
		return BadRequest("cannot decode request metadata: %v", err)
	}
	if payload.Action != "write" {
		return BadRequest(`multipart action must be "write", got %q`, payload.Action)
	}
	if len(payload.Files) == 0 {
		return BadRequest("must specify one or more files")
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
			return BadRequest("cannot read file part %d: %v", i, err)
		}
		if part.FormName() != "files" {
			return BadRequest(`field name must be "files", got %q`, part.FormName())
		}
		path := multipartFilename(part)
		info, ok := infos[path]
		if !ok {
			return BadRequest("no metadata for path %q", path)
		}
		errors[path] = writeFile(info, part)
		part.Close()
	}

	// Build list of results with any errors.
	result := make([]fileResult, len(payload.Files))
	for i, file := range payload.Files {
		err, ok := errors[file.Path]
		if !ok {
			// Ensure we wrote all the files in the metadata.
			err = fmt.Errorf("no file content for path %q", file.Path)
		}
		result[i] = fileResult{
			Path:  file.Path,
			Error: fileErrorToResult(err),
		}
	}
	return SyncResponse(result)
}

// This is equivalent to part.FileName(), but in Go 1.17 that was changed to
// call filepath.Base() on the result, stripping off the path, which our API
// depends on. So roll our own, equivalent to the Go 1.16 version.
func multipartFilename(part *multipart.Part) string {
	contentDisposition := part.Header.Get("Content-Disposition")
	_, params, _ := mime.ParseMediaType(contentDisposition)
	return params["filename"]
}

func writeFile(item writeFilesItem, source io.Reader) error {
	if !pathpkg.IsAbs(item.Path) {
		return nonAbsolutePathError(item.Path)
	}

	uid, gid, err := normalizeUidGid(item.UserID, item.GroupID, item.User, item.Group)
	if err != nil {
		return fmt.Errorf("cannot look up user and group: %w", err)
	}

	// Create parent directory if needed.
	if item.MakeDirs {
		err := mkdirAllUserGroup(pathpkg.Dir(item.Path), 0o755, uid, gid)
		if err != nil {
			return fmt.Errorf("cannot create directory: %w", err)
		}
	}

	// Atomically write file content to destination.
	perm, err := parsePermissions(item.Permissions, 0o644)
	if err != nil {
		return err
	}
	sysUid, sysGid := sys.UserID(osutil.NoChown), sys.GroupID(osutil.NoChown)
	if uid != nil && gid != nil {
		sysUid, sysGid = sys.UserID(*uid), sys.GroupID(*gid)
	}
	return atomicWriteChown(item.Path, source, perm, osutil.AtomicWriteChmod, sysUid, sysGid)
}

func mkdirAllUserGroup(path string, perm os.FileMode, uid, gid *int) error {
	if uid != nil && gid != nil {
		return mkdirAllChown(path, perm, sys.UserID(*uid), sys.GroupID(*gid))
	} else {
		return mkdirAllChown(path, perm, osutil.NoChown, osutil.NoChown)
	}
}

func mkdirUserGroup(path string, perm os.FileMode, uid, gid *int) error {
	if uid != nil && gid != nil {
		return mkdirChown(path, perm, sys.UserID(*uid), sys.GroupID(*gid))
	} else {
		return mkdirChown(path, perm, osutil.NoChown, osutil.NoChown)
	}
}

func parsePermissions(permissions string, defaultMode os.FileMode) (os.FileMode, error) {
	if permissions == "" {
		return defaultMode, nil
	}
	perm, err := strconv.ParseUint(permissions, 8, 32)
	if err != nil || len(permissions) != 3 {
		return 0, fmt.Errorf("permissions must be a 3-digit octal string, got %q", permissions)
	}
	return os.FileMode(perm), nil
}

// Creating directories

type makeDirsItem struct {
	Path        string `json:"path"`
	MakeParents bool   `json:"make-parents"`
	Permissions string `json:"permissions"`
	UserID      *int   `json:"user-id"`
	User        string `json:"user"`
	GroupID     *int   `json:"group-id"`
	Group       string `json:"group"`
}

func makeDirs(dirs []makeDirsItem) Response {
	result := make([]fileResult, len(dirs))
	for i, dir := range dirs {
		err := makeDir(dir)
		result[i] = fileResult{
			Path:  dir.Path,
			Error: fileErrorToResult(err),
		}
	}
	return SyncResponse(result)
}

func makeDir(dir makeDirsItem) error {
	if !pathpkg.IsAbs(dir.Path) {
		return nonAbsolutePathError(dir.Path)
	}
	perm, err := parsePermissions(dir.Permissions, 0o755)
	if err != nil {
		return err
	}
	uid, gid, err := normalizeUidGid(dir.UserID, dir.GroupID, dir.User, dir.Group)
	if err != nil {
		return fmt.Errorf("cannot look up user and group: %w", err)
	}
	if dir.MakeParents {
		err = mkdirAllUserGroup(dir.Path, perm, uid, gid)
	} else {
		err = mkdirUserGroup(dir.Path, perm, uid, gid)
	}
	if err != nil {
		return err
	}
	return nil
}

// Because it's hard to test os.Chown without running the tests as root.
var (
	atomicWriteChown = osutil.AtomicWriteChown
	normalizeUidGid  = osutil.NormalizeUidGid
	mkdirChown       = osutil.MkdirChown
	mkdirAllChown    = osutil.MkdirAllChown
)

// Removing paths

type removePathsItem struct {
	Path      string `json:"path"`
	Recursive bool   `json:"recursive"`
}

func removePaths(paths []removePathsItem) Response {
	result := make([]fileResult, len(paths))
	for i, path := range paths {
		err := removePath(path.Path, path.Recursive)
		result[i] = fileResult{
			Path:  path.Path,
			Error: fileErrorToResult(err),
		}
	}
	return SyncResponse(result)
}

func removePath(path string, recursive bool) error {
	if !pathpkg.IsAbs(path) {
		return nonAbsolutePathError(path)
	}
	if recursive {
		return os.RemoveAll(path)
	}
	return os.Remove(path)
}
