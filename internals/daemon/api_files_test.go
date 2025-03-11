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
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/osutil"
	"github.com/canonical/pebble/internals/osutil/sys"
)

var _ = Suite(&filesSuite{})

type filesSuite struct{}

func (s *filesSuite) TestGetFilesInvalidAction(c *C) {
	query := url.Values{"action": []string{"foo"}}
	response, body := doRequest(c, v1GetFiles, "GET", "/v1/files", query, nil, nil)
	c.Assert(response.StatusCode, Equals, http.StatusBadRequest)
	assertError(c, body, http.StatusBadRequest, "", `invalid action "foo"`)
}

func (s *filesSuite) TestListFilesNoPath(c *C) {
	query := url.Values{
		"action": []string{"list"},
		"path":   []string{""},
	}
	response, body := doRequest(c, v1GetFiles, "GET", "/v1/files", query, nil, nil)
	c.Assert(response.StatusCode, Equals, http.StatusBadRequest)
	assertError(c, body, http.StatusBadRequest, "", `must specify path`)
}

func (s *filesSuite) TestListFilesNonAbsPath(c *C) {
	query := url.Values{
		"action": []string{"list"},
		"path":   []string{"bar"},
	}
	response, body := doRequest(c, v1GetFiles, "GET", "/v1/files", query, nil, nil)
	c.Assert(response.StatusCode, Equals, http.StatusBadRequest)
	assertError(c, body, http.StatusBadRequest, "", `path must be absolute, got .*`)
}

func (s *filesSuite) TestListFilesPermissionDenied(c *C) {
	if os.Getuid() == 0 {
		c.Skip("cannot run test as root")
	}
	tmpDir := c.MkDir()
	noAccessDir := filepath.Join(tmpDir, "noaccess")
	c.Assert(os.Mkdir(noAccessDir, 0o775), IsNil)
	c.Assert(os.Chmod(noAccessDir, 0), IsNil)

	query := url.Values{
		"action": []string{"list"},
		"path":   []string{noAccessDir},
	}
	response, body := doRequest(c, v1GetFiles, "GET", "/v1/files", query, nil, nil)
	c.Assert(response.StatusCode, Equals, http.StatusForbidden)
	assertError(c, body, http.StatusForbidden, "permission-denied", ".*: permission denied")
}

func (s *filesSuite) TestListFilesNotFound(c *C) {
	tmpDir := createTestFiles(c)

	for _, pattern := range []string{tmpDir + "/notfound", tmpDir + "/*.xyz"} {
		query := url.Values{
			"action": []string{"list"},
			"path":   []string{pattern},
		}
		response, body := doRequest(c, v1GetFiles, "GET", "/v1/files", query, nil, nil)
		c.Assert(response.StatusCode, Equals, http.StatusNotFound)
		assertError(c, body, http.StatusNotFound, "not-found", ".* no such file or directory")
	}
}

func (s *filesSuite) TestListFilesDir(c *C) {
	tmpDir := createTestFiles(c)

	for _, pattern := range []string{"", "*"} {
		query := url.Values{
			"action":  []string{"list"},
			"path":    []string{tmpDir},
			"pattern": []string{pattern},
		}
		response, body := doRequest(c, v1GetFiles, "GET", "/v1/files", query, nil, nil)
		c.Assert(response.StatusCode, Equals, http.StatusOK)

		r := decodeResp(c, body, http.StatusOK, ResponseTypeSync)
		assertListResult(c, r.Result, 0, "file", tmpDir, "foo", "644", 1)
		assertListResult(c, r.Result, 1, "file", tmpDir, "one.txt", "600", 2)
		assertListResult(c, r.Result, 2, "directory", tmpDir, "sub", "755", -1)
		assertListResult(c, r.Result, 3, "file", tmpDir, "two.txt", "755", 3)
	}
}

func (s *filesSuite) TestListFilesDirItself(c *C) {
	tmpDir := createTestFiles(c)

	query := url.Values{
		"action": []string{"list"},
		"path":   []string{tmpDir + "/sub"},
		"itself": []string{"true"},
	}
	response, body := doRequest(c, v1GetFiles, "GET", "/v1/files", query, nil, nil)
	c.Assert(response.StatusCode, Equals, http.StatusOK)

	r := decodeResp(c, body, http.StatusOK, ResponseTypeSync)
	assertListResult(c, r.Result, 0, "directory", tmpDir, "sub", "755", -1)
}

func (s *filesSuite) TestListFilesWithPattern(c *C) {
	tmpDir := createTestFiles(c)

	query := url.Values{
		"action":  []string{"list"},
		"path":    []string{tmpDir},
		"pattern": []string{"*.txt"},
	}
	response, body := doRequest(c, v1GetFiles, "GET", "/v1/files", query, nil, nil)
	c.Assert(response.StatusCode, Equals, http.StatusOK)

	r := decodeResp(c, body, http.StatusOK, ResponseTypeSync)
	assertListResult(c, r.Result, 0, "file", tmpDir, "one.txt", "600", 2)
	assertListResult(c, r.Result, 1, "file", tmpDir, "two.txt", "755", 3)
}

func (s *filesSuite) TestListFilesFile(c *C) {
	tmpDir := createTestFiles(c)

	query := url.Values{
		"action": []string{"list"},
		"path":   []string{tmpDir + "/foo"},
	}
	response, body := doRequest(c, v1GetFiles, "GET", "/v1/files", query, nil, nil)
	c.Assert(response.StatusCode, Equals, http.StatusOK)

	r := decodeResp(c, body, http.StatusOK, ResponseTypeSync)
	assertListResult(c, r.Result, 0, "file", tmpDir, "foo", "644", 1)
}

func (s *filesSuite) TestListFilesNoResults(c *C) {
	tmpDir := c.MkDir()

	query := url.Values{
		"action": []string{"list"},
		"path":   []string{tmpDir},
	}
	response, body := doRequest(c, v1GetFiles, "GET", "/v1/files", query, nil, nil)
	c.Assert(response.StatusCode, Equals, http.StatusOK)

	r := decodeResp(c, body, http.StatusOK, ResponseTypeSync)
	c.Assert(r.Result, HasLen, 0) // should be empty slice, not nil
}

func (s *filesSuite) TestReadNoPaths(c *C) {
	query := url.Values{"action": []string{"read"}}
	response, body := doRequest(c, v1GetFiles, "GET", "/v1/files", query, nil, nil)
	c.Assert(response.StatusCode, Equals, http.StatusBadRequest)
	assertError(c, body, http.StatusBadRequest, "", "must specify one or more paths")
}

func (s *filesSuite) TestReadNoMultipartHeader(c *C) {
	query := url.Values{"action": []string{"read"}, "path": []string{"foo"}}
	response, body := doRequest(c, v1GetFiles, "GET", "/v1/files", query, nil, nil)
	c.Assert(response.StatusCode, Equals, http.StatusBadRequest)
	assertError(c, body, http.StatusBadRequest, "", "must accept multipart/form-data")
}

type testFileResult struct {
	Path  string
	Error struct {
		Kind    string
		Message string
	}
}

type testFilesResponse struct {
	Type       string
	StatusCode int `json:"status-code"`
	Status     string
	Result     []testFileResult
}

func (s *filesSuite) TestReadSingle(c *C) {
	tmpDir := createTestFiles(c)

	query := url.Values{
		"action": []string{"read"},
		"path":   []string{tmpDir + "/one.txt"},
	}
	headers := http.Header{
		"Accept": []string{"multipart/form-data"},
	}
	response, body := doRequest(c, v1GetFiles, "GET", "/v1/files", query, headers, nil)
	c.Check(response.StatusCode, Equals, http.StatusOK)

	var r testFilesResponse
	files := readMultipart(c, response, body, &r)
	c.Check(r.Type, Equals, "sync")
	c.Check(r.StatusCode, Equals, http.StatusOK)
	c.Check(r.Status, Equals, "OK")
	c.Check(r.Result, HasLen, 1)
	checkFileResult(c, r.Result[0], tmpDir+"/one.txt", "", "")

	c.Check(files, DeepEquals, map[string]string{
		tmpDir + "/one.txt": "be",
	})
}

func (s *filesSuite) TestReadErrorOnRead(c *C) {
	// You can open /proc/self/mem with error, but when you read from it
	// at offset 0 you get a Read error -- this tests that code path.
	f, err := os.Open("/proc/self/mem")
	if err != nil {
		c.Skip("/proc/self/mem unavailable")
	}
	_ = f.Close()

	query := url.Values{
		"action": []string{"read"},
		"path":   []string{"/proc/self/mem"},
	}
	headers := http.Header{
		"Accept": []string{"multipart/form-data"},
	}
	response, body := doRequest(c, v1GetFiles, "GET", "/v1/files", query, headers, nil)
	c.Check(response.StatusCode, Equals, http.StatusOK)

	var r testFilesResponse
	files := readMultipart(c, response, body, &r)
	c.Check(r.Type, Equals, "sync")
	c.Check(r.StatusCode, Equals, http.StatusOK)
	c.Check(r.Status, Equals, "OK")
	c.Check(r.Result, HasLen, 1)
	checkFileResult(c, r.Result[0], "/proc/self/mem", "generic-file-error", ".*input/output error")

	// File will still be in response, but with no content
	c.Check(files, DeepEquals, map[string]string{
		"/proc/self/mem": "",
	})
}

func checkFileResult(c *C, r testFileResult, path, errorKind, errorMsg string) {
	c.Check(r.Path, Equals, path)
	c.Check(r.Error.Kind, Equals, errorKind)
	c.Check(r.Error.Message, Matches, errorMsg)
}

func (s *filesSuite) TestReadMultiple(c *C) {
	tmpDir := createTestFiles(c)

	query := url.Values{
		"action": []string{"read"},
		"path":   []string{tmpDir + "/foo", tmpDir + "/one.txt", tmpDir + "/two.txt"},
	}
	headers := http.Header{
		"Accept": []string{"multipart/form-data"},
	}
	response, body := doRequest(c, v1GetFiles, "GET", "/v1/files", query, headers, nil)
	c.Check(response.StatusCode, Equals, http.StatusOK)

	var r testFilesResponse
	files := readMultipart(c, response, body, &r)
	c.Check(r.Type, Equals, "sync")
	c.Check(r.StatusCode, Equals, http.StatusOK)
	c.Check(r.Status, Equals, "OK")
	c.Check(r.Result, HasLen, 3)
	checkFileResult(c, r.Result[0], tmpDir+"/foo", "", "")
	checkFileResult(c, r.Result[1], tmpDir+"/one.txt", "", "")
	checkFileResult(c, r.Result[2], tmpDir+"/two.txt", "", "")

	c.Check(files, DeepEquals, map[string]string{
		tmpDir + "/foo":     "a",
		tmpDir + "/one.txt": "be",
		tmpDir + "/two.txt": "cee",
	})
}

func (s *filesSuite) TestReadErrors(c *C) {
	if os.Getuid() == 0 {
		c.Skip("cannot run test as root")
	}

	tmpDir := createTestFiles(c)
	writeTempFile(c, tmpDir, "no-access", "x", 0)

	query := url.Values{
		"action": []string{"read"},
		"path": []string{
			tmpDir + "/no-exist",
			tmpDir + "/foo", // successful read
			tmpDir + "/no-access",
			"relative-path",
			tmpDir,
		},
	}
	headers := http.Header{
		"Accept": []string{"multipart/form-data"},
	}
	response, body := doRequest(c, v1GetFiles, "GET", "/v1/files", query, headers, nil)
	c.Check(response.StatusCode, Equals, http.StatusOK) // actual HTTP status is 200

	var r testFilesResponse
	files := readMultipart(c, response, body, &r)
	c.Check(r.Type, Equals, "sync")
	c.Check(r.StatusCode, Equals, http.StatusOK)
	c.Check(r.Status, Equals, "OK")
	c.Check(r.Result, HasLen, 5)
	checkFileResult(c, r.Result[0], tmpDir+"/no-exist", "not-found", ".*: no such file or directory")
	checkFileResult(c, r.Result[1], tmpDir+"/foo", "", "")
	checkFileResult(c, r.Result[2], tmpDir+"/no-access", "permission-denied", ".*: permission denied")
	checkFileResult(c, r.Result[3], "relative-path", "generic-file-error", "paths must be absolute, got .*")
	checkFileResult(c, r.Result[4], tmpDir, "generic-file-error", "can only read a regular file: .*")

	c.Check(files, DeepEquals, map[string]string{
		tmpDir + "/foo": "a",
	})
}

func (s *filesSuite) TestPostFilesInvalidContentType(c *C) {
	response, body := doRequest(c, v1PostFiles, "POST", "/v1/files", nil, nil, nil)
	c.Check(response.StatusCode, Equals, http.StatusBadRequest)
	assertError(c, body, http.StatusBadRequest, "", `invalid Content-Type ""`)

	headers := http.Header{
		"Content-Type": []string{"text/foo"},
	}
	response, body = doRequest(c, v1PostFiles, "POST", "/v1/files", nil, headers, nil)
	c.Check(response.StatusCode, Equals, http.StatusBadRequest)
	assertError(c, body, http.StatusBadRequest, "", `invalid media type "text/foo"`)
}

func (s *filesSuite) TestPostFilesInvalidAction(c *C) {
	headers := http.Header{
		"Content-Type": []string{"application/json"},
	}
	response, body := doRequest(c, v1PostFiles, "POST", "/v1/files", nil, headers,
		[]byte(`{"action": "foo"}`))
	c.Check(response.StatusCode, Equals, http.StatusBadRequest)
	assertError(c, body, http.StatusBadRequest, "", `invalid action "foo"`)

	response, body = doRequest(c, v1PostFiles, "POST", "/v1/files", nil, headers,
		[]byte(`{"action": "write"}`))
	c.Check(response.StatusCode, Equals, http.StatusBadRequest)
	assertError(c, body, http.StatusBadRequest, "", `must use multipart with "write" action`)
}

func (s *filesSuite) TestMakeDirsSingle(c *C) {
	tmpDir := c.MkDir()

	headers := http.Header{
		"Content-Type": []string{"application/json"},
	}
	payload := struct {
		Action string
		Dirs   []makeDirsItem
	}{
		Action: "make-dirs",
		Dirs: []makeDirsItem{
			{Path: tmpDir + "/newdir"},
		},
	}
	reqBody, err := json.Marshal(payload)
	c.Assert(err, IsNil)
	response, body := doRequest(c, v1PostFiles, "POST", "/v1/files", nil, headers, reqBody)
	c.Check(response.StatusCode, Equals, http.StatusOK)

	var r testFilesResponse
	c.Assert(json.NewDecoder(body).Decode(&r), IsNil)
	c.Check(r.StatusCode, Equals, http.StatusOK)
	c.Check(r.Type, Equals, "sync")
	c.Check(r.Result, HasLen, 1)
	checkFileResult(c, r.Result[0], tmpDir+"/newdir", "", "")

	c.Check(osutil.IsDir(tmpDir+"/newdir"), Equals, true)
}

func (s *filesSuite) TestMakeDirsMultiple(c *C) {
	tmpDir := c.MkDir()

	headers := http.Header{
		"Content-Type": []string{"application/json"},
	}
	payload := struct {
		Action string
		Dirs   []makeDirsItem
	}{
		Action: "make-dirs",
		Dirs: []makeDirsItem{
			{Path: tmpDir + "/newdir"},
			{Path: tmpDir + "/will/not/work"},
			{Path: tmpDir + "/make/my/parents", MakeParents: true, Permissions: "700"},
		},
	}
	reqBody, err := json.Marshal(payload)
	c.Assert(err, IsNil)
	response, body := doRequest(c, v1PostFiles, "POST", "/v1/files", nil, headers, reqBody)
	c.Check(response.StatusCode, Equals, http.StatusOK)

	var r testFilesResponse
	c.Assert(json.NewDecoder(body).Decode(&r), IsNil)
	c.Check(r.StatusCode, Equals, http.StatusOK)
	c.Check(r.Type, Equals, "sync")
	c.Check(r.Result, HasLen, 3)
	checkFileResult(c, r.Result[0], tmpDir+"/newdir", "", "")
	checkFileResult(c, r.Result[1], tmpDir+"/will/not/work", "not-found", ".*")
	checkFileResult(c, r.Result[2], tmpDir+"/make/my/parents", "", "")

	c.Check(osutil.IsDir(tmpDir+"/newdir"), Equals, true)
	c.Check(osutil.IsDir(tmpDir+"/will/not/work"), Equals, false)
	c.Check(osutil.IsDir(tmpDir+"/make/my/parents"), Equals, true)
	st, err := os.Stat(tmpDir + "/newdir")
	c.Assert(err, IsNil)
	c.Check(st.Mode().Perm(), Equals, os.FileMode(0o755))
	st, err = os.Stat(tmpDir + "/make/my/parents")
	c.Assert(err, IsNil)
	c.Check(st.Mode().Perm(), Equals, os.FileMode(0o700))
}

func (s *filesSuite) TestMakeDirsUserGroupMocked(c *C) {
	type args struct {
		path    string
		perm    os.FileMode
		options osutil.MkdirOptions
	}
	var mkdirCalls []args
	mkdir = func(path string, perm os.FileMode, options *osutil.MkdirOptions) error {
		if options == nil {
			options = &osutil.MkdirOptions{}
		}
		mkdirCalls = append(mkdirCalls, args{path, perm, *options})
		return os.MkdirAll(path, perm)
	}

	normalizeUidGid = func(uid, gid *int, username, group string) (*int, *int, error) {
		if uid != nil {
			return uid, gid, nil
		}
		if username == "" {
			return nil, nil, nil
		}
		c.Check(username, Equals, "USER")
		c.Check(group, Equals, "GROUP")
		u, g := 56, 78
		return &u, &g, nil
	}

	defer func() {
		mkdir = osutil.Mkdir
		normalizeUidGid = osutil.NormalizeUidGid
	}()

	tmpDir := s.testMakeDirsUserGroup(c, 12, 34, "USER", "GROUP")

	c.Assert(mkdirCalls, HasLen, 5)
	c.Check(mkdirCalls[0], Equals, args{tmpDir + "/normal", 0o755, osutil.MkdirOptions{Chmod: true}})
	c.Check(mkdirCalls[1], Equals, args{tmpDir + "/uid-gid", 0o755, osutil.MkdirOptions{Chmod: true, Chown: true, UserID: 12, GroupID: 34}})
	c.Check(mkdirCalls[2], Equals, args{tmpDir + "/user-group", 0o755, osutil.MkdirOptions{Chmod: true, Chown: true, UserID: 56, GroupID: 78}})
	c.Check(mkdirCalls[3], Equals, args{tmpDir + "/nested1/normal", 0o755, osutil.MkdirOptions{MakeParents: true, ExistOK: true, Chmod: true}})
	c.Check(mkdirCalls[4], Equals, args{tmpDir + "/nested2/user-group", 0o755, osutil.MkdirOptions{MakeParents: true, ExistOK: true, Chmod: true, Chown: true, UserID: 56, GroupID: 78}})
}

func (s *filesSuite) testMakeDirsUserGroup(c *C, uid, gid int, user, group string) string {
	tmpDir := c.MkDir()

	headers := http.Header{
		"Content-Type": []string{"application/json"},
	}
	payload := struct {
		Action string
		Dirs   []makeDirsItem
	}{
		Action: "make-dirs",
		Dirs: []makeDirsItem{
			{Path: tmpDir + "/normal"},
			{Path: tmpDir + "/uid-gid", UserID: &uid, GroupID: &gid},
			{Path: tmpDir + "/user-group", User: user, Group: group},
			{Path: tmpDir + "/nested1/normal", MakeParents: true},
			{Path: tmpDir + "/nested2/user-group", User: user, Group: group, MakeParents: true},
		},
	}
	reqBody, err := json.Marshal(payload)
	c.Assert(err, IsNil)
	response, body := doRequest(c, v1PostFiles, "POST", "/v1/files", nil, headers, reqBody)
	c.Check(response.StatusCode, Equals, http.StatusOK)

	var r testFilesResponse
	c.Assert(json.NewDecoder(body).Decode(&r), IsNil)
	c.Check(r.StatusCode, Equals, http.StatusOK)
	c.Check(r.Type, Equals, "sync")
	c.Check(r.Result, HasLen, 5)
	checkFileResult(c, r.Result[0], tmpDir+"/normal", "", "")
	checkFileResult(c, r.Result[1], tmpDir+"/uid-gid", "", "")
	checkFileResult(c, r.Result[2], tmpDir+"/user-group", "", "")
	checkFileResult(c, r.Result[3], tmpDir+"/nested1/normal", "", "")
	checkFileResult(c, r.Result[4], tmpDir+"/nested2/user-group", "", "")

	c.Check(osutil.IsDir(tmpDir+"/normal"), Equals, true)
	c.Check(osutil.IsDir(tmpDir+"/uid-gid"), Equals, true)
	c.Check(osutil.IsDir(tmpDir+"/user-group"), Equals, true)
	c.Check(osutil.IsDir(tmpDir+"/nested1/normal"), Equals, true)
	c.Check(osutil.IsDir(tmpDir+"/nested2/user-group"), Equals, true)

	return tmpDir
}

func (s *filesSuite) TestMakeDirsUserGroupReal(c *C) {
	if os.Getuid() != 0 {
		c.Skip("requires running as root")
	}
	username := os.Getenv("PEBBLE_TEST_USER")
	group := os.Getenv("PEBBLE_TEST_GROUP")
	if username == "" || group == "" {
		c.Fatalf("must set PEBBLE_TEST_USER and PEBBLE_TEST_GROUP")
	}
	u, err := user.Lookup(username)
	c.Assert(err, IsNil)
	g, err := user.LookupGroup(group)
	c.Assert(err, IsNil)
	uid, err := strconv.Atoi(u.Uid)
	c.Assert(err, IsNil)
	gid, err := strconv.Atoi(g.Gid)
	c.Assert(err, IsNil)

	tmpDir := s.testMakeDirsUserGroup(c, uid, gid, username, group)

	info, err := os.Stat(tmpDir + "/normal")
	c.Assert(err, IsNil)
	statT := info.Sys().(*syscall.Stat_t)
	c.Check(statT.Uid, Equals, uint32(0))
	c.Check(statT.Gid, Equals, uint32(0))

	info, err = os.Stat(tmpDir + "/uid-gid")
	c.Assert(err, IsNil)
	statT = info.Sys().(*syscall.Stat_t)
	c.Check(statT.Uid, Equals, uint32(uid))
	c.Check(statT.Gid, Equals, uint32(uid))

	info, err = os.Stat(tmpDir + "/user-group")
	c.Assert(err, IsNil)
	statT = info.Sys().(*syscall.Stat_t)
	c.Check(statT.Uid, Equals, uint32(uid))
	c.Check(statT.Gid, Equals, uint32(uid))

	info, err = os.Stat(tmpDir + "/nested1")
	c.Assert(err, IsNil)
	c.Check(int(info.Mode()&os.ModePerm), Equals, 0o755)
	statT = info.Sys().(*syscall.Stat_t)
	c.Check(statT.Uid, Equals, uint32(0))
	c.Check(statT.Gid, Equals, uint32(0))

	info, err = os.Stat(tmpDir + "/nested1/normal")
	c.Assert(err, IsNil)
	statT = info.Sys().(*syscall.Stat_t)
	c.Check(statT.Uid, Equals, uint32(0))
	c.Check(statT.Gid, Equals, uint32(0))

	info, err = os.Stat(tmpDir + "/nested2")
	c.Assert(err, IsNil)
	c.Check(int(info.Mode()&os.ModePerm), Equals, 0o755)
	statT = info.Sys().(*syscall.Stat_t)
	c.Check(statT.Uid, Equals, uint32(uid))
	c.Check(statT.Gid, Equals, uint32(gid))

	info, err = os.Stat(tmpDir + "/nested2/user-group")
	c.Assert(err, IsNil)
	statT = info.Sys().(*syscall.Stat_t)
	c.Check(statT.Uid, Equals, uint32(uid))
	c.Check(statT.Gid, Equals, uint32(gid))
}

func (s *filesSuite) TestRemoveSingle(c *C) {
	tmpDir := c.MkDir()
	writeTempFile(c, tmpDir, "file", "a", 0o644)

	headers := http.Header{
		"Content-Type": []string{"application/json"},
	}
	payload := struct {
		Action string
		Paths  []removePathsItem
	}{
		Action: "remove",
		Paths: []removePathsItem{
			{Path: tmpDir + "/file"},
		},
	}
	reqBody, err := json.Marshal(payload)
	c.Assert(err, IsNil)
	response, body := doRequest(c, v1PostFiles, "POST", "/v1/files", nil, headers, reqBody)
	c.Check(response.StatusCode, Equals, http.StatusOK)

	var r testFilesResponse
	c.Assert(json.NewDecoder(body).Decode(&r), IsNil)
	c.Check(r.StatusCode, Equals, http.StatusOK)
	c.Check(r.Type, Equals, "sync")
	c.Check(r.Result, HasLen, 1)
	checkFileResult(c, r.Result[0], tmpDir+"/file", "", "")

	c.Check(osutil.CanStat(tmpDir+"/file"), Equals, false)
}

func (s *filesSuite) TestRemoveMultiple(c *C) {
	tmpDir := c.MkDir()
	writeTempFile(c, tmpDir, "file", "a", 0o644)
	c.Assert(os.Mkdir(tmpDir+"/empty", 0o755), IsNil)
	c.Assert(os.Mkdir(tmpDir+"/non-empty", 0o755), IsNil)
	writeTempFile(c, tmpDir, "non-empty/bar", "b", 0o644)
	c.Assert(os.Mkdir(tmpDir+"/recursive", 0o755), IsNil)
	writeTempFile(c, tmpDir, "recursive/car", "c", 0o644)

	headers := http.Header{
		"Content-Type": []string{"application/json"},
	}
	payload := struct {
		Action string
		Paths  []removePathsItem
	}{
		Action: "remove",
		Paths: []removePathsItem{
			{Path: tmpDir + "/file"},
			{Path: tmpDir + "/empty"},
			{Path: tmpDir + "/non-empty"},
			{Path: tmpDir + "/recursive", Recursive: true},
		},
	}
	reqBody, err := json.Marshal(payload)
	c.Assert(err, IsNil)
	response, body := doRequest(c, v1PostFiles, "POST", "/v1/files", nil, headers, reqBody)
	c.Check(response.StatusCode, Equals, http.StatusOK)

	var r testFilesResponse
	c.Assert(json.NewDecoder(body).Decode(&r), IsNil)
	c.Check(r.StatusCode, Equals, http.StatusOK)
	c.Check(r.Type, Equals, "sync")
	c.Check(r.Result, HasLen, 4)
	checkFileResult(c, r.Result[0], tmpDir+"/file", "", "")
	checkFileResult(c, r.Result[1], tmpDir+"/empty", "", "")
	checkFileResult(c, r.Result[2], tmpDir+"/non-empty", "generic-file-error", ".*directory not empty")
	checkFileResult(c, r.Result[3], tmpDir+"/recursive", "", "")

	c.Check(osutil.CanStat(tmpDir+"/file"), Equals, false)
	c.Check(osutil.IsDir(tmpDir+"/empty"), Equals, false)
	c.Check(osutil.IsDir(tmpDir+"/non-empty"), Equals, true)
	c.Check(osutil.IsDir(tmpDir+"/recursive"), Equals, false)
}

func (s *filesSuite) TestWriteNoMetadata(c *C) {
	headers := http.Header{
		"Content-Type": []string{"multipart/form-data; boundary=01234567890123456789012345678901"},
	}
	response, body := doRequest(c, v1PostFiles, "POST", "/v1/files", nil, headers, []byte{})
	c.Check(response.StatusCode, Equals, http.StatusBadRequest)
	assertError(c, body, http.StatusBadRequest, "", `cannot read request metadata: .*`)
}

func (s *filesSuite) TestWriteInvalidBoundary(c *C) {
	headers := http.Header{
		"Content-Type": []string{"multipart/form-data"},
	}
	response, body := doRequest(c, v1PostFiles, "POST", "/v1/files", nil, headers, []byte{})
	c.Check(response.StatusCode, Equals, http.StatusBadRequest)
	assertError(c, body, http.StatusBadRequest, "", `invalid boundary ""`)

	headers = http.Header{
		"Content-Type": []string{"multipart/form-data; boundary=SHORT"},
	}
	response, body = doRequest(c, v1PostFiles, "POST", "/v1/files", nil, headers, []byte{})
	c.Check(response.StatusCode, Equals, http.StatusBadRequest)
	assertError(c, body, http.StatusBadRequest, "", `invalid boundary "SHORT"`)
}

func (s *filesSuite) TestWriteInvalidRequestField(c *C) {
	headers := http.Header{
		"Content-Type": []string{"multipart/form-data; boundary=01234567890123456789012345678901"},
	}
	response, body := doRequest(c, v1PostFiles, "POST", "/v1/files", nil, headers,
		[]byte(`
--01234567890123456789012345678901
Content-Disposition: form-data; name="foo"

{"foo": "bar"}
--01234567890123456789012345678901--
`))
	c.Check(response.StatusCode, Equals, http.StatusBadRequest)
	assertError(c, body, http.StatusBadRequest, "", `metadata field name must be "request", got "foo"`)
}

func (s *filesSuite) TestWriteInvalidFileField(c *C) {
	headers := http.Header{
		"Content-Type": []string{"multipart/form-data; boundary=01234567890123456789012345678901"},
	}
	response, body := doRequest(c, v1PostFiles, "POST", "/v1/files", nil, headers,
		[]byte(`
--01234567890123456789012345678901
Content-Disposition: form-data; name="request"

{"action": "write", "files": [
	{"path": "/foo/bar"}
]}
--01234567890123456789012345678901
Content-Disposition: form-data; name="bad"; filename="foo"

Bad file field
--01234567890123456789012345678901--
`))
	c.Check(response.StatusCode, Equals, http.StatusBadRequest)
	assertError(c, body, http.StatusBadRequest, "", `field name must be "files", got "bad"`)
}

func (s *filesSuite) TestWriteNoMetadataForPath(c *C) {
	headers := http.Header{
		"Content-Type": []string{"multipart/form-data; boundary=01234567890123456789012345678901"},
	}
	response, body := doRequest(c, v1PostFiles, "POST", "/v1/files", nil, headers,
		[]byte(`
--01234567890123456789012345678901
Content-Disposition: form-data; name="request"

{"action": "write", "files": [
	{"path": "/foo/bar"}
]}
--01234567890123456789012345678901
Content-Disposition: form-data; name="files"; filename="/no-metadata"

No metadata
--01234567890123456789012345678901--
`))
	c.Check(response.StatusCode, Equals, http.StatusBadRequest)
	assertError(c, body, http.StatusBadRequest, "", `no metadata for path "/no-metadata"`)
}

func (s *filesSuite) TestWriteInvalidMetadata(c *C) {
	headers := http.Header{
		"Content-Type": []string{"multipart/form-data; boundary=01234567890123456789012345678901"},
	}
	response, body := doRequest(c, v1PostFiles, "POST", "/v1/files", nil, headers,
		[]byte(`
--01234567890123456789012345678901
Content-Disposition: form-data; name="request"

not json
--01234567890123456789012345678901--
`))
	c.Check(response.StatusCode, Equals, http.StatusBadRequest)
	assertError(c, body, http.StatusBadRequest, "", `cannot decode request metadata.*`)

	response, body = doRequest(c, v1PostFiles, "POST", "/v1/files", nil, headers,
		[]byte(`
--01234567890123456789012345678901
Content-Disposition: form-data; name="request"

{"action": "foo"}
--01234567890123456789012345678901--
`))
	c.Check(response.StatusCode, Equals, http.StatusBadRequest)
	assertError(c, body, http.StatusBadRequest, "", `multipart action must be "write", got "foo"`)
}

func (s *filesSuite) TestWriteNoFiles(c *C) {
	headers := http.Header{
		"Content-Type": []string{"multipart/form-data; boundary=01234567890123456789012345678901"},
	}
	response, body := doRequest(c, v1PostFiles, "POST", "/v1/files", nil, headers,
		[]byte(`
--01234567890123456789012345678901
Content-Disposition: form-data; name="request"

{"action": "write"}
--01234567890123456789012345678901--
`))
	c.Check(response.StatusCode, Equals, http.StatusBadRequest)
	assertError(c, body, http.StatusBadRequest, "", "must specify one or more files")
}

func (s *filesSuite) TestWriteSingle(c *C) {
	tmpDir := c.MkDir()
	path := tmpDir + "/hello.txt"

	headers := http.Header{
		"Content-Type": []string{"multipart/form-data; boundary=01234567890123456789012345678901"},
	}
	response, body := doRequest(c, v1PostFiles, "POST", "/v1/files", nil, headers,
		[]byte(fmt.Sprintf(`
--01234567890123456789012345678901
Content-Disposition: form-data; name="request"

{"action": "write", "files": [
	{"path": "%[1]s"}
]}
--01234567890123456789012345678901
Content-Disposition: form-data; name="files"; filename="%[1]s"

Hello world
--01234567890123456789012345678901--
`, path)))
	c.Check(response.StatusCode, Equals, http.StatusOK)

	var r testFilesResponse
	c.Assert(json.NewDecoder(body).Decode(&r), IsNil)
	c.Check(r.StatusCode, Equals, http.StatusOK)
	c.Check(r.Type, Equals, "sync")
	c.Check(r.Result, HasLen, 1)
	checkFileResult(c, r.Result[0], path, "", "")

	assertFile(c, path, 0o644, "Hello world")
}

func (s *filesSuite) TestWriteOverwrite(c *C) {
	tmpDir := c.MkDir()
	path := tmpDir + "/hello.txt"

	for _, content := range []string{"Hello", "byebye"} {
		headers := http.Header{
			"Content-Type": []string{"multipart/form-data; boundary=01234567890123456789012345678901"},
		}
		response, body := doRequest(c, v1PostFiles, "POST", "/v1/files", nil, headers,
			[]byte(fmt.Sprintf(`
--01234567890123456789012345678901
Content-Disposition: form-data; name="request"

{"action": "write", "files": [
	{"path": "%[1]s"}
]}
--01234567890123456789012345678901
Content-Disposition: form-data; name="files"; filename="%[1]s"

%[2]s
--01234567890123456789012345678901--
`, path, content)))
		c.Check(response.StatusCode, Equals, http.StatusOK)

		var r testFilesResponse
		c.Assert(json.NewDecoder(body).Decode(&r), IsNil)
		c.Check(r.StatusCode, Equals, http.StatusOK)
		c.Check(r.Type, Equals, "sync")
		c.Check(r.Result, HasLen, 1)
		checkFileResult(c, r.Result[0], path, "", "")

		assertFile(c, path, 0o644, content)
	}
}

func (s *filesSuite) TestWriteMultiple(c *C) {
	// Ensure non-zero umask to test the files API's explicit chmod.  This is
	// also why one file's permissions are 777 - to ensure the umasked
	// permissions are overwridden as expected.
	oldmask := syscall.Umask(0002)
	defer syscall.Umask(oldmask)

	tmpDir := c.MkDir()
	path0 := tmpDir + "/hello.txt"
	path1 := tmpDir + "/byebye.txt"
	path2 := tmpDir + "/foo/bar.txt"

	headers := http.Header{
		"Content-Type": []string{"multipart/form-data; boundary=01234567890123456789012345678901"},
	}
	response, body := doRequest(c, v1PostFiles, "POST", "/v1/files", nil, headers,
		[]byte(fmt.Sprintf(`
--01234567890123456789012345678901
Content-Disposition: form-data; name="request"

{
	"action": "write",
	"files": [
		{"path": "%[1]s"},
		{"path": "%[2]s", "permissions": "777"},
		{"path": "%[3]s", "make-dirs": true}
	]
}
--01234567890123456789012345678901
Content-Disposition: form-data; name="files"; filename="%[1]s"

Hello
--01234567890123456789012345678901
Content-Disposition: form-data; name="files"; filename="%[2]s"

Bye bye
--01234567890123456789012345678901
Content-Disposition: form-data; name="files"; filename="%[3]s"

Foo
Bar
--01234567890123456789012345678901--
`, path0, path1, path2)))
	c.Check(response.StatusCode, Equals, http.StatusOK)

	var r testFilesResponse
	c.Assert(json.NewDecoder(body).Decode(&r), IsNil)
	c.Check(r.StatusCode, Equals, http.StatusOK)
	c.Check(r.Type, Equals, "sync")
	c.Check(r.Result, HasLen, 3)
	checkFileResult(c, r.Result[0], path0, "", "")
	checkFileResult(c, r.Result[1], path1, "", "")
	checkFileResult(c, r.Result[2], path2, "", "")

	assertFile(c, path0, 0o644, "Hello")
	assertFile(c, path1, 0o777, "Bye bye")
	assertFile(c, path2, 0o644, "Foo\nBar")
	info, err := os.Stat(tmpDir + "/foo")
	c.Assert(err, IsNil)
	c.Assert(info.Mode().Perm(), Equals, os.FileMode(0o755))
}

func (s *filesSuite) TestWriteUserGroupMocked(c *C) {
	type args struct {
		name string
		perm os.FileMode
		uid  sys.UserID
		gid  sys.GroupID
	}
	var atomicWriteChownCalls []args
	atomicWriteChown = func(name string, r io.Reader, perm os.FileMode, flags osutil.AtomicWriteFlags, uid sys.UserID, gid sys.GroupID) error {
		atomicWriteChownCalls = append(atomicWriteChownCalls, args{name, perm, uid, gid})
		return osutil.AtomicWrite(name, r, perm, flags)
	}

	normalizeUidGid = func(uid, gid *int, username, group string) (*int, *int, error) {
		if uid != nil {
			return uid, gid, nil
		}
		if username == "" {
			return nil, nil, nil
		}
		c.Check(username, Equals, "USER")
		c.Check(group, Equals, "GROUP")
		u, g := 56, 78
		return &u, &g, nil
	}

	type mkdirArgs struct {
		name    string
		perm    os.FileMode
		options osutil.MkdirOptions
	}
	var mkdirCalls []mkdirArgs
	mkdir = func(path string, perm os.FileMode, options *osutil.MkdirOptions) error {
		mkdirCalls = append(mkdirCalls, mkdirArgs{path, perm, *options})
		return os.MkdirAll(path, perm)
	}

	defer func() {
		atomicWriteChown = osutil.AtomicWriteChown
		normalizeUidGid = osutil.NormalizeUidGid
		mkdir = osutil.Mkdir
	}()

	tmpDir := s.testWriteUserGroup(c, 12, 34, "USER", "GROUP")

	c.Assert(atomicWriteChownCalls, HasLen, 5)
	c.Check(atomicWriteChownCalls[0], Equals, args{tmpDir + "/normal", 0o644, osutil.NoChown, osutil.NoChown})
	c.Check(atomicWriteChownCalls[1], Equals, args{tmpDir + "/uid-gid", 0o644, 12, 34})
	c.Check(atomicWriteChownCalls[2], Equals, args{tmpDir + "/user-group", 0o644, 56, 78})
	c.Check(atomicWriteChownCalls[3], Equals, args{tmpDir + "/nested1/normal", 0o644, osutil.NoChown, osutil.NoChown})
	c.Check(atomicWriteChownCalls[4], Equals, args{tmpDir + "/nested2/user-group", 0o644, 56, 78})

	c.Assert(mkdirCalls, HasLen, 2)
	c.Check(mkdirCalls[0], Equals, mkdirArgs{tmpDir + "/nested1", 0o755, osutil.MkdirOptions{MakeParents: true, ExistOK: true, Chmod: true}})
	c.Check(mkdirCalls[1], Equals, mkdirArgs{tmpDir + "/nested2", 0o755, osutil.MkdirOptions{MakeParents: true, ExistOK: true, Chmod: true, Chown: true, UserID: 56, GroupID: 78}})
}

func (s *filesSuite) TestWriteUserGroupReal(c *C) {
	if os.Getuid() != 0 {
		c.Skip("requires running as root")
	}
	username := os.Getenv("PEBBLE_TEST_USER")
	group := os.Getenv("PEBBLE_TEST_GROUP")
	if username == "" || group == "" {
		c.Fatalf("must set PEBBLE_TEST_USER and PEBBLE_TEST_GROUP")
	}
	u, err := user.Lookup(username)
	c.Assert(err, IsNil)
	g, err := user.LookupGroup(group)
	c.Assert(err, IsNil)
	uid, err := strconv.Atoi(u.Uid)
	c.Assert(err, IsNil)
	gid, err := strconv.Atoi(g.Gid)
	c.Assert(err, IsNil)

	tmpDir := s.testWriteUserGroup(c, uid, gid, username, group)

	info, err := os.Stat(tmpDir + "/normal")
	c.Assert(err, IsNil)
	statT := info.Sys().(*syscall.Stat_t)
	c.Check(statT.Uid, Equals, uint32(0))
	c.Check(statT.Gid, Equals, uint32(0))

	info, err = os.Stat(tmpDir + "/uid-gid")
	c.Assert(err, IsNil)
	statT = info.Sys().(*syscall.Stat_t)
	c.Check(statT.Uid, Equals, uint32(uid))
	c.Check(statT.Gid, Equals, uint32(uid))

	info, err = os.Stat(tmpDir + "/user-group")
	c.Assert(err, IsNil)
	statT = info.Sys().(*syscall.Stat_t)
	c.Check(statT.Uid, Equals, uint32(uid))
	c.Check(statT.Gid, Equals, uint32(uid))

	info, err = os.Stat(tmpDir + "/nested1")
	c.Assert(err, IsNil)
	c.Check(int(info.Mode()&os.ModePerm), Equals, 0o755)
	statT = info.Sys().(*syscall.Stat_t)
	c.Check(statT.Uid, Equals, uint32(0))
	c.Check(statT.Gid, Equals, uint32(0))

	info, err = os.Stat(tmpDir + "/nested1/normal")
	c.Assert(err, IsNil)
	statT = info.Sys().(*syscall.Stat_t)
	c.Check(statT.Uid, Equals, uint32(0))
	c.Check(statT.Gid, Equals, uint32(0))

	info, err = os.Stat(tmpDir + "/nested2")
	c.Assert(err, IsNil)
	c.Check(int(info.Mode()&os.ModePerm), Equals, 0o755)
	statT = info.Sys().(*syscall.Stat_t)
	c.Check(statT.Uid, Equals, uint32(uid))
	c.Check(statT.Gid, Equals, uint32(gid))

	info, err = os.Stat(tmpDir + "/nested2/user-group")
	c.Assert(err, IsNil)
	statT = info.Sys().(*syscall.Stat_t)
	c.Check(statT.Uid, Equals, uint32(uid))
	c.Check(statT.Gid, Equals, uint32(gid))
}

func (s *filesSuite) testWriteUserGroup(c *C, uid, gid int, user, group string) string {
	tmpDir := c.MkDir()
	pathNormal := tmpDir + "/normal"
	pathUidGid := tmpDir + "/uid-gid"
	pathUserGroup := tmpDir + "/user-group"
	pathNested := tmpDir + "/nested1/normal"
	pathNestedUserGroup := tmpDir + "/nested2/user-group"

	headers := http.Header{
		"Content-Type": []string{"multipart/form-data; boundary=01234567890123456789012345678901"},
	}
	response, body := doRequest(c, v1PostFiles, "POST", "/v1/files", nil, headers,
		[]byte(fmt.Sprintf(`
--01234567890123456789012345678901
Content-Disposition: form-data; name="request"

{
	"action": "write",
	"files": [
		{"path": "%[1]s"},
		{"path": "%[2]s", "user-id": %[3]d, "group-id": %[4]d},
		{"path": "%[5]s", "user": "%[6]s", "group": "%[7]s"},
		{"path": "%[8]s", "make-dirs": true},
		{"path": "%[9]s", "user": "%[10]s", "group": "%[11]s", "make-dirs": true}
	]
}
--01234567890123456789012345678901
Content-Disposition: form-data; name="files"; filename="%[1]s"

normal
--01234567890123456789012345678901
Content-Disposition: form-data; name="files"; filename="%[2]s"

uid gid
--01234567890123456789012345678901
Content-Disposition: form-data; name="files"; filename="%[5]s"

user group
--01234567890123456789012345678901
Content-Disposition: form-data; name="files"; filename="%[8]s"

nested
--01234567890123456789012345678901
Content-Disposition: form-data; name="files"; filename="%[9]s"

nested user group
--01234567890123456789012345678901--
`, pathNormal, pathUidGid, uid, gid, pathUserGroup, user, group,
			pathNested, pathNestedUserGroup, user, group)))
	c.Check(response.StatusCode, Equals, http.StatusOK)

	var r testFilesResponse
	c.Assert(json.NewDecoder(body).Decode(&r), IsNil)
	c.Check(r.StatusCode, Equals, http.StatusOK)
	c.Check(r.Type, Equals, "sync")
	c.Check(r.Result, HasLen, 5)
	checkFileResult(c, r.Result[0], pathNormal, "", "")
	checkFileResult(c, r.Result[1], pathUidGid, "", "")
	checkFileResult(c, r.Result[2], pathUserGroup, "", "")
	checkFileResult(c, r.Result[3], pathNested, "", "")
	checkFileResult(c, r.Result[4], pathNestedUserGroup, "", "")

	assertFile(c, pathNormal, 0o644, "normal")
	assertFile(c, pathUidGid, 0o644, "uid gid")
	assertFile(c, pathUserGroup, 0o644, "user group")
	assertFile(c, pathNested, 0o644, "nested")
	assertFile(c, pathNestedUserGroup, 0o644, "nested user group")

	return tmpDir
}

func (s *filesSuite) TestWriteErrors(c *C) {
	if os.Getuid() == 0 {
		c.Skip("cannot run test as root")
	}

	tmpDir := c.MkDir()
	c.Assert(os.Mkdir(tmpDir+"/permission-denied", 0), IsNil)
	pathNoContent := tmpDir + "/no-content"
	pathNotAbsolute := "path-not-absolute"
	pathNotFound := tmpDir + "/not-found/foo"
	pathPermissionDenied := tmpDir + "/permission-denied/file"
	pathUserNotFound := tmpDir + "/user-not-found"
	pathGroupNotFound := tmpDir + "/group-not-found"

	headers := http.Header{
		"Content-Type": []string{"multipart/form-data; boundary=01234567890123456789012345678901"},
	}
	response, body := doRequest(c, v1PostFiles, "POST", "/v1/files", nil, headers,
		[]byte(fmt.Sprintf(`
--01234567890123456789012345678901
Content-Disposition: form-data; name="request"

{
	"action": "write",
	"files": [
		{"path": "%[1]s"},
		{"path": "%[2]s"},
		{"path": "%[3]s"},
		{"path": "%[4]s"},
		{"path": "%[5]s", "user": "user-not-found", "group": "nogroup"},
		{"path": "%[6]s", "user": "nobody", "group": "group-not-found"}
	]
}
--01234567890123456789012345678901
Content-Disposition: form-data; name="files"; filename="%[2]s"

path not absolute
--01234567890123456789012345678901
Content-Disposition: form-data; name="files"; filename="%[3]s"

dir not found
--01234567890123456789012345678901
Content-Disposition: form-data; name="files"; filename="%[4]s"

permission denied
--01234567890123456789012345678901
Content-Disposition: form-data; name="files"; filename="%[5]s"

user not found
--01234567890123456789012345678901
Content-Disposition: form-data; name="files"; filename="%[6]s"

group not found
--01234567890123456789012345678901--
`, pathNoContent, pathNotAbsolute, pathNotFound, pathPermissionDenied,
			pathUserNotFound, pathGroupNotFound)))
	c.Check(response.StatusCode, Equals, http.StatusOK)

	var r testFilesResponse
	c.Assert(json.NewDecoder(body).Decode(&r), IsNil)
	c.Check(r.StatusCode, Equals, http.StatusOK)
	c.Check(r.Type, Equals, "sync")
	c.Check(r.Result, HasLen, 6)
	checkFileResult(c, r.Result[0], pathNoContent, "generic-file-error", "no file content for path.*")
	checkFileResult(c, r.Result[1], pathNotAbsolute, "generic-file-error", "paths must be absolute, got .*")
	checkFileResult(c, r.Result[2], pathNotFound, "not-found", ".*")
	checkFileResult(c, r.Result[3], pathPermissionDenied, "permission-denied", ".*")
	checkFileResult(c, r.Result[4], pathUserNotFound, "generic-file-error", ".*unknown user.*")
	checkFileResult(c, r.Result[5], pathGroupNotFound, "generic-file-error", ".*unknown group.*")

	c.Check(osutil.CanStat(pathNoContent), Equals, false)
	c.Check(osutil.CanStat(pathNotAbsolute), Equals, false)
	c.Check(osutil.CanStat(pathNotFound), Equals, false)
	c.Check(osutil.CanStat(pathPermissionDenied), Equals, false)
}

func assertFile(c *C, path string, perm os.FileMode, content string) {
	b, err := os.ReadFile(path)
	c.Assert(err, IsNil)
	c.Assert(string(b), Equals, content)
	info, err := os.Stat(path)
	c.Assert(err, IsNil)
	c.Assert(info.Mode().Perm(), Equals, perm)
}

// Read a multipart HTTP response body, parse JSON in "response" field to result,
// and return map of file field to file content.
func readMultipart(c *C, response *http.Response, body io.Reader, result any) map[string]string {
	contentType := response.Header.Get("Content-Type")
	mediaType, params, err := mime.ParseMediaType(contentType)
	c.Assert(err, IsNil)
	c.Assert(mediaType, Equals, "multipart/form-data")
	c.Assert(params["boundary"], Not(Equals), "")
	mr := multipart.NewReader(body, params["boundary"])

	// First decode all the files
	files := make(map[string]string)
	var part *multipart.Part
	for {
		part, err = mr.NextPart()
		c.Assert(err, IsNil)
		if part.FormName() != "files" {
			break
		}
		b, err := io.ReadAll(part)
		c.Assert(err, IsNil)
		files[multipartFilename(part)] = string(b)
		part.Close()
	}

	// Then decode "response" JSON at the end
	c.Assert(part.FormName(), Equals, "response")
	decoder := json.NewDecoder(part)
	err = decoder.Decode(result)
	c.Assert(err, IsNil)
	part.Close()

	// Ensure that was the last part
	_, err = mr.NextPart()
	c.Assert(err, Equals, io.EOF)

	return files
}

func doRequest(c *C, f ResponseFunc, method, url string, query url.Values, headers http.Header, body []byte) (*http.Response, *bytes.Buffer) {
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewBuffer(body)
	}
	req, err := http.NewRequest(method, url, bodyReader)
	c.Assert(err, IsNil)
	if query != nil {
		req.URL.RawQuery = query.Encode()
	}
	req.Header = headers
	handler := f(apiCmd(url), req, nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	response := recorder.Result()
	return response, recorder.Body
}

func assertError(c *C, body io.Reader, status int, kind, message string) {
	var r respJSON
	c.Assert(json.NewDecoder(body).Decode(&r), IsNil)
	c.Assert(r.Status, Equals, status)
	c.Assert(r.StatusText, Equals, http.StatusText(status))
	c.Assert(r.Type, Equals, ResponseTypeError)
	result := r.Result.(map[string]any)
	if kind != "" {
		c.Assert(result["kind"], Equals, kind)
	}
	c.Assert(result["message"], Matches, message)
}

func createTestFiles(c *C) string {
	tmpDir := c.MkDir()
	writeTempFile(c, tmpDir, "foo", "a", 0o644)
	writeTempFile(c, tmpDir, "one.txt", "be", 0o600)
	c.Assert(os.Mkdir(tmpDir+"/sub", 0o755), IsNil)
	writeTempFile(c, tmpDir, "two.txt", "cee", 0o755)
	return tmpDir
}

func writeTempFile(c *C, dir, filename, content string, perm os.FileMode) {
	err := os.WriteFile(filepath.Join(dir, filename), []byte(content), perm)
	c.Assert(err, IsNil)
}

func assertListResult(c *C, result any, index int, typ, dir, name, perms string, size int) {
	x := result.([]any)[index].(map[string]any)
	c.Assert(x["type"], Equals, typ)
	c.Assert(x["name"], Equals, name)
	c.Assert(x["path"], Equals, filepath.Join(dir, name))
	c.Assert(x["permissions"], Equals, perms)
	if size >= 0 {
		c.Assert(int(x["size"].(float64)), Equals, size)
	} else {
		_, ok := x["size"]
		c.Assert(ok, Equals, false)
	}
	_, err := time.Parse(time.RFC3339, x["last-modified"].(string))
	c.Assert(err, IsNil)

	uid := int(x["user-id"].(float64))
	c.Assert(uid, Equals, os.Getuid())
	gid := int(x["group-id"].(float64))
	c.Assert(gid, Equals, os.Getgid())

	usr, err := user.LookupId(strconv.Itoa(uid))
	c.Assert(err, IsNil)
	c.Assert(x["user"], Equals, usr.Username)
	group, err := user.LookupGroupId(strconv.Itoa(gid))
	c.Assert(err, IsNil)
	c.Assert(x["group"], Equals, group.Name)
}

func decodeResp(c *C, body io.Reader, status int, typ ResponseType) respJSON {
	var r respJSON
	c.Assert(json.NewDecoder(body).Decode(&r), IsNil)
	c.Assert(r.Status, Equals, status)
	c.Assert(r.StatusText, Equals, http.StatusText(status))
	c.Assert(r.Type, Equals, typ)
	return r
}
