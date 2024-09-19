//go:build roottest

// Copyright (c) 2024 Canonical Ltd
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
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/user"
	"strconv"
	"syscall"
	"testing"

	"github.com/canonical/pebble/internals/osutil"
)

func TestWithRootMakeDirsUserGroupReal(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("requires running as root")
	}
	username := os.Getenv("PEBBLE_TEST_USER")
	group := os.Getenv("PEBBLE_TEST_GROUP")
	if username == "" || group == "" {
		t.Fatalf("must set PEBBLE_TEST_USER and PEBBLE_TEST_GROUP")
	}
	u, err := user.Lookup(username)
	if err != nil {
		t.Fatalf("cannot look up username: %v", err)
	}
	g, err := user.LookupGroup(group)
	if err != nil {
		t.Fatalf("cannot look up group: %v", err)
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		t.Fatalf("cannot convert uid to int: %v", err)
	}
	gid, err := strconv.Atoi(g.Gid)
	if err != nil {
		t.Fatalf("cannot convert gid to int: %v", err)
	}

	tmpDir := testWithRootMakeDirsUserGroup(t, uid, gid, username, group)

	info, err := os.Stat(tmpDir + "/normal")
	if err != nil {
		t.Fatalf("cannot stat dir %s: %v", tmpDir+"/normal", err)
	}
	statT := info.Sys().(*syscall.Stat_t)
	if statT.Uid != uint32(0) {
		t.Fatalf("dir %s uid error, expected: %v, got: %v", tmpDir+"/normal", uint32(0), statT.Uid)
	}
	if statT.Gid != uint32(0) {
		t.Fatalf("dir %s gid error, expected: %v, got: %v", tmpDir+"/normal", uint32(0), statT.Uid)
	}

	info, err = os.Stat(tmpDir + "/uid-gid")
	if err != nil {
		t.Fatalf("cannot stat dir %s: %v", tmpDir+"/uid-gid", err)
	}
	statT = info.Sys().(*syscall.Stat_t)
	if statT.Uid != uint32(uid) {
		t.Fatalf("dir %s uid error, expected: %v, got: %v", tmpDir+"/uid-gid", uint32(uid), statT.Uid)
	}
	if statT.Gid != uint32(gid) {
		t.Fatalf("dir %s gid error, expected: %v, got: %v", tmpDir+"/uid-gid", uint32(gid), statT.Uid)
	}

	info, err = os.Stat(tmpDir + "/user-group")
	if err != nil {
		t.Fatalf("cannot stat dir %s: %v", tmpDir+"/user-group", err)
	}
	statT = info.Sys().(*syscall.Stat_t)
	if statT.Uid != uint32(uid) {
		t.Fatalf("dir %s uid error, expected: %v, got: %v", tmpDir+"/user-group", uint32(uid), statT.Uid)
	}
	if statT.Gid != uint32(gid) {
		t.Fatalf("dir %s gid error, expected: %v, got: %v", tmpDir+"/user-group", uint32(gid), statT.Uid)
	}

	info, err = os.Stat(tmpDir + "/nested1")
	if err != nil {
		t.Fatalf("cannot stat dir %s: %v", tmpDir+"/nested1", err)
	}
	if int(info.Mode()&os.ModePerm) != 0o755 {
		t.Fatalf("dir %s mode error, expected: %v, got: %v", tmpDir+"/nested1", 0o755, int(info.Mode()&os.ModePerm))
	}
	statT = info.Sys().(*syscall.Stat_t)
	if statT.Uid != uint32(0) {
		t.Fatalf("dir %s uid error, expected: %v, got: %v", tmpDir+"/nested1", uint32(0), statT.Uid)
	}
	if statT.Gid != uint32(0) {
		t.Fatalf("dir %s gid error, expected: %v, got: %v", tmpDir+"/nested1", uint32(0), statT.Uid)
	}

	info, err = os.Stat(tmpDir + "/nested1/normal")
	if err != nil {
		t.Fatalf("cannot stat dir %s: %v", tmpDir+"/nested1/normal", err)
	}
	statT = info.Sys().(*syscall.Stat_t)
	if statT.Uid != uint32(0) {
		t.Fatalf("dir %s uid error, expected: %v, got: %v", tmpDir+"/nested1/normal", uint32(0), statT.Uid)
	}
	if statT.Gid != uint32(0) {
		t.Fatalf("dir %s gid error, expected: %v, got: %v", tmpDir+"/nested1/normal", uint32(0), statT.Uid)
	}

	info, err = os.Stat(tmpDir + "/nested2")
	if err != nil {
		t.Fatalf("cannot stat dir %s: %v", tmpDir+"/nested2", err)
	}
	if int(info.Mode()&os.ModePerm) != 0o755 {
		t.Fatalf("dir %s mode error, expected: %v, got: %v", tmpDir+"/nested2", 0o755, int(info.Mode()&os.ModePerm))
	}
	statT = info.Sys().(*syscall.Stat_t)
	if statT.Uid != uint32(uid) {
		t.Fatalf("dir %s uid error, expected: %v, got: %v", tmpDir+"/nested2", uint32(uid), statT.Uid)
	}
	if statT.Gid != uint32(gid) {
		t.Fatalf("dir %s gid error, expected: %v, got: %v", tmpDir+"/nested2", uint32(gid), statT.Uid)
	}

	info, err = os.Stat(tmpDir + "/nested2/user-group")
	if err != nil {
		t.Fatalf("cannot stat dir %s: %v", tmpDir+"/nested2/user-group", err)
	}
	statT = info.Sys().(*syscall.Stat_t)
	if statT.Uid != uint32(uid) {
		t.Fatalf("dir %s uid error, expected: %v, got: %v", tmpDir+"/nested2/user-group", uint32(uid), statT.Uid)
	}
	if statT.Gid != uint32(gid) {
		t.Fatalf("dir %s gid error, expected: %v, got: %v", tmpDir+"/nested2/user-group", uint32(gid), statT.Uid)
	}
}

func testWithRootMakeDirsUserGroup(t *testing.T, uid, gid int, user, group string) string {
	tmpDir := t.TempDir()

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
	if err != nil {
		t.Fatalf("cannot marshal payload: %v", err)
	}
	body := doRequestRootTest(t, v1PostFiles, "POST", "/v1/files", nil, headers, reqBody)

	var r testFilesResponse
	if err := json.NewDecoder(body).Decode(&r); err != nil {
		t.Fatalf("cannot decode response body for /v1/files: %v", err)
	}
	if r.StatusCode != http.StatusOK {
		t.Fatalf("test file response status code error, expected: %v, got %v", http.StatusOK, r.StatusCode)

	}
	if r.Type != "sync" {
		t.Fatalf("test file response type error, expected: sync, got %v", r.StatusCode)

	}
	if len(r.Result) != 5 {
		t.Fatalf("test file response result length error, expected: 5, got %v", len(r.Result))

	}
	checkFileResultRootTest(t, r.Result[0], tmpDir+"/normal", "", "")
	checkFileResultRootTest(t, r.Result[1], tmpDir+"/uid-gid", "", "")
	checkFileResultRootTest(t, r.Result[2], tmpDir+"/user-group", "", "")
	checkFileResultRootTest(t, r.Result[3], tmpDir+"/nested1/normal", "", "")
	checkFileResultRootTest(t, r.Result[4], tmpDir+"/nested2/user-group", "", "")

	if !osutil.IsDir(tmpDir + "/normal") {
		t.Fatalf("file %s is not a directory", tmpDir+"/normal")

	}
	if !osutil.IsDir(tmpDir + "/uid-gid") {
		t.Fatalf("file %s is not a directory", tmpDir+"/uid-gid")

	}
	if !osutil.IsDir(tmpDir + "/user-group") {
		t.Fatalf("file %s is not a directory", tmpDir+"/user-group")

	}
	if !osutil.IsDir(tmpDir + "/nested1/normal") {
		t.Fatalf("file %s is not a directory", tmpDir+"/nested1/normal")

	}
	if !osutil.IsDir(tmpDir + "/nested2/user-group") {
		t.Fatalf("file %s is not a directory", tmpDir+"/nested2/user-group")

	}

	return tmpDir
}

func doRequestRootTest(t *testing.T, f ResponseFunc, method, url string, query url.Values, headers http.Header, body []byte) *bytes.Buffer {
	t.Helper()

	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewBuffer(body)
	}
	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		t.Fatalf("http request error: %s", err)
	}
	if query != nil {
		req.URL.RawQuery = query.Encode()
	}
	req.Header = headers
	handler := f(apiCmd(url), req, nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	response := recorder.Result()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("http request to %s failed: %v", url, err)
	}
	return recorder.Body
}

func checkFileResultRootTest(t *testing.T, r testFileResult, path, errorKind, errorMsg string) {
	t.Helper()

	if r.Path != path {
		t.Fatalf("error checking test file path, eexpected: %v, got: %v", path, r.Path)
	}
	if r.Error.Kind != errorKind {
		t.Fatalf("error checking test file error kind, eexpected: %v, got: %v", errorKind, r.Error.Kind)
	}
	if r.Error.Message != errorMsg {
		t.Fatalf("error checking test file error message, eexpected: %v, got: %v", errorMsg, r.Error.Message)
	}
}

func TestWithRootWriteUserGroupReal(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("requires running as root")
	}
	username := os.Getenv("PEBBLE_TEST_USER")
	group := os.Getenv("PEBBLE_TEST_GROUP")
	if username == "" || group == "" {
		t.Fatalf("must set PEBBLE_TEST_USER and PEBBLE_TEST_GROUP")
	}
	u, err := user.Lookup(username)
	if err != nil {
		t.Fatalf("cannot look up username: %v", err)
	}
	g, err := user.LookupGroup(group)
	if err != nil {
		t.Fatalf("cannot look up group: %v", err)
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		t.Fatalf("cannot convert uid to int: %v", err)
	}
	gid, err := strconv.Atoi(g.Gid)
	if err != nil {
		t.Fatalf("cannot convert gid to int: %v", err)
	}

	tmpDir := testWriteUserGroupRootTest(t, uid, gid, username, group)

	info, err := os.Stat(tmpDir + "/normal")
	if err != nil {
		t.Fatalf("cannot stat file %s: %v", tmpDir+"/normal", err)
	}
	statT := info.Sys().(*syscall.Stat_t)
	if statT.Uid != uint32(0) {
		t.Fatalf("file %s uid error, expected: %v, got: %v", tmpDir+"/normal", uint32(0), statT.Uid)
	}
	if statT.Gid != uint32(0) {
		t.Fatalf("file %s gid error, expected: %v, got: %v", tmpDir+"/normal", uint32(0), statT.Uid)
	}

	info, err = os.Stat(tmpDir + "/uid-gid")
	if err != nil {
		t.Fatalf("cannot stat file %s: %v", tmpDir+"/uid-gid", err)
	}
	statT = info.Sys().(*syscall.Stat_t)
	if statT.Uid != uint32(uid) {
		t.Fatalf("file %s uid error, expected: %v, got: %v", tmpDir+"/uid-gid", uint32(uid), statT.Uid)
	}
	if statT.Gid != uint32(gid) {
		t.Fatalf("file %s gid error, expected: %v, got: %v", tmpDir+"/uid-gid", uint32(gid), statT.Uid)
	}

	info, err = os.Stat(tmpDir + "/user-group")
	if err != nil {
		t.Fatalf("cannot stat file %s: %v", tmpDir+"/user-group", err)
	}
	statT = info.Sys().(*syscall.Stat_t)
	if statT.Uid != uint32(uid) {
		t.Fatalf("file %s uid error, expected: %v, got: %v", tmpDir+"/user-group", uint32(uid), statT.Uid)
	}
	if statT.Gid != uint32(gid) {
		t.Fatalf("file %s gid error, expected: %v, got: %v", tmpDir+"/user-group", uint32(gid), statT.Uid)
	}

	info, err = os.Stat(tmpDir + "/nested1")
	if err != nil {
		t.Fatalf("cannot stat file %s: %v", tmpDir+"/nested1", err)
	}
	if int(info.Mode()&os.ModePerm) != 0o755 {
		t.Fatalf("file %s mode error, expected: %v, got: %v", tmpDir+"/nested1", 0o755, int(info.Mode()&os.ModePerm))
	}
	statT = info.Sys().(*syscall.Stat_t)
	if statT.Uid != uint32(0) {
		t.Fatalf("file %s uid error, expected: %v, got: %v", tmpDir+"/nested1", uint32(0), statT.Uid)
	}
	if statT.Gid != uint32(0) {
		t.Fatalf("file %s gid error, expected: %v, got: %v", tmpDir+"/nested1", uint32(0), statT.Uid)
	}

	info, err = os.Stat(tmpDir + "/nested1/normal")
	if err != nil {
		t.Fatalf("cannot stat file %s: %v", tmpDir+"/nested1/normal", err)
	}
	statT = info.Sys().(*syscall.Stat_t)
	if statT.Uid != uint32(0) {
		t.Fatalf("dir %s uid error, expected: %v, got: %v", tmpDir+"/nested1/normal", uint32(0), statT.Uid)
	}
	if statT.Gid != uint32(0) {
		t.Fatalf("dir %s gid error, expected: %v, got: %v", tmpDir+"/nested1/normal", uint32(0), statT.Uid)
	}

	info, err = os.Stat(tmpDir + "/nested2")
	if err != nil {
		t.Fatalf("cannot stat file %s: %v", tmpDir+"/nested2", err)
	}
	if int(info.Mode()&os.ModePerm) != 0o755 {
		t.Fatalf("file %s mode error, expected: %v, got: %v", tmpDir+"/nested2", 0o755, int(info.Mode()&os.ModePerm))
	}
	statT = info.Sys().(*syscall.Stat_t)
	if statT.Uid != uint32(uid) {
		t.Fatalf("file %s uid error, expected: %v, got: %v", tmpDir+"/nested2", uint32(uid), statT.Uid)
	}
	if statT.Gid != uint32(gid) {
		t.Fatalf("file %s gid error, expected: %v, got: %v", tmpDir+"/nested2", uint32(gid), statT.Uid)
	}

	info, err = os.Stat(tmpDir + "/nested2/user-group")
	if err != nil {
		t.Fatalf("cannot stat file %s: %v", tmpDir+"/nested2/user-group", err)
	}
	statT = info.Sys().(*syscall.Stat_t)
	if statT.Uid != uint32(uid) {
		t.Fatalf("file %s uid error, expected: %v, got: %v", tmpDir+"/nested2/user-group", uint32(uid), statT.Uid)
	}
	if statT.Gid != uint32(gid) {
		t.Fatalf("file %s gid error, expected: %v, got: %v", tmpDir+"/nested2/user-group", uint32(gid), statT.Uid)
	}
}

func testWriteUserGroupRootTest(t *testing.T, uid, gid int, user, group string) string {
	tmpDir := t.TempDir()
	pathNormal := tmpDir + "/normal"
	pathUidGid := tmpDir + "/uid-gid"
	pathUserGroup := tmpDir + "/user-group"
	pathNested := tmpDir + "/nested1/normal"
	pathNestedUserGroup := tmpDir + "/nested2/user-group"

	headers := http.Header{
		"Content-Type": []string{"multipart/form-data; boundary=01234567890123456789012345678901"},
	}
	body := doRequestRootTest(t, v1PostFiles, "POST", "/v1/files", nil, headers,
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

	var r testFilesResponse
	if err := json.NewDecoder(body).Decode(&r); err != nil {
		t.Fatalf("cannot decode response body for /v1/files: %v", err)
	}
	if r.StatusCode != http.StatusOK {
		t.Fatalf("test file response status code error, expected: %v, got %v", http.StatusOK, r.StatusCode)

	}
	if r.Type != "sync" {
		t.Fatalf("test file response type error, expected: sync, got %v", r.StatusCode)

	}
	if len(r.Result) != 5 {
		t.Fatalf("test file response result length error, expected: 5, got %v", len(r.Result))

	}
	checkFileResultRootTest(t, r.Result[0], pathNormal, "", "")
	checkFileResultRootTest(t, r.Result[1], pathUidGid, "", "")
	checkFileResultRootTest(t, r.Result[2], pathUserGroup, "", "")
	checkFileResultRootTest(t, r.Result[3], pathNested, "", "")
	checkFileResultRootTest(t, r.Result[4], pathNestedUserGroup, "", "")

	assertFileRootTest(t, pathNormal, 0o644, "normal")
	assertFileRootTest(t, pathUidGid, 0o644, "uid gid")
	assertFileRootTest(t, pathUserGroup, 0o644, "user group")
	assertFileRootTest(t, pathNested, 0o644, "nested")
	assertFileRootTest(t, pathNestedUserGroup, 0o644, "nested user group")

	return tmpDir
}

func assertFileRootTest(t *testing.T, path string, perm os.FileMode, content string) {
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read file %s: %v", path, err)
	}
	if string(b) != content {
		t.Fatalf("file content error, expected: %v, got: %v", content, string(b))
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("cannot stat file %s: %v", path, err)
	}
	if info.Mode().Perm() != perm {
		t.Fatalf("error checking permission, expected: %v, got: %v", perm, info.Mode().Perm())
	}
}
