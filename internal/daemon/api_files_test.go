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
	"io/ioutil"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/canonical/pebble/internal/osutil"
	. "gopkg.in/check.v1"
)

var _ = Suite(&filesSuite{})

type filesSuite struct{}

func (s *filesSuite) SetUpTest(c *C) {
}

func (s *filesSuite) TearDownTest(c *C) {
}

func (s *filesSuite) TestGetFilesInvalidAction(c *C) {
	query := url.Values{"action": []string{"foo"}}
	response, body := doRequest(c, v1GetFiles, "GET", "/v1/files", query, nil, nil)
	c.Assert(response.StatusCode, Equals, http.StatusBadRequest)
	assertError(c, body, http.StatusBadRequest, "", `invalid action "foo"`)
}

func (s *filesSuite) TestListFilesNoPattern(c *C) {
	query := url.Values{
		"action":  []string{"list"},
		"pattern": []string{""},
	}
	response, body := doRequest(c, v1GetFiles, "GET", "/v1/files", query, nil, nil)
	c.Assert(response.StatusCode, Equals, http.StatusBadRequest)
	assertError(c, body, http.StatusBadRequest, "", `must specify pattern`)
}

func (s *filesSuite) TestListFilesNonAbsPattern(c *C) {
	query := url.Values{
		"action":  []string{"list"},
		"pattern": []string{"bar"},
	}
	response, body := doRequest(c, v1GetFiles, "GET", "/v1/files", query, nil, nil)
	c.Assert(response.StatusCode, Equals, http.StatusBadRequest)
	assertError(c, body, http.StatusBadRequest, "", `pattern must be absolute .*`)
}

func (s *filesSuite) TestListFilesPermissionDenied(c *C) {
	tmpDir := c.MkDir()
	noAccessDir := filepath.Join(tmpDir, "noaccess")
	c.Assert(os.Mkdir(noAccessDir, 0o775), IsNil)
	c.Assert(os.Chmod(noAccessDir, 0), IsNil)

	query := url.Values{
		"action":  []string{"list"},
		"pattern": []string{noAccessDir},
	}
	response, body := doRequest(c, v1GetFiles, "GET", "/v1/files", query, nil, nil)
	c.Assert(response.StatusCode, Equals, http.StatusForbidden)
	assertError(c, body, http.StatusForbidden, "permission-denied", ".*: permission denied")
}

func (s *filesSuite) TestListFilesNotFound(c *C) {
	tmpDir := createTestFiles(c)

	for _, pattern := range []string{tmpDir + "/notfound", tmpDir + "/*.xyz"} {
		query := url.Values{
			"action":  []string{"list"},
			"pattern": []string{pattern},
		}
		response, body := doRequest(c, v1GetFiles, "GET", "/v1/files", query, nil, nil)
		c.Assert(response.StatusCode, Equals, http.StatusNotFound)
		assertError(c, body, http.StatusNotFound, "not-found", "file does not exist")
	}
}

func (s *filesSuite) TestListFilesDir(c *C) {
	tmpDir := createTestFiles(c)

	for _, pattern := range []string{tmpDir, tmpDir + "/*"} {
		query := url.Values{
			"action":  []string{"list"},
			"pattern": []string{pattern},
		}
		response, body := doRequest(c, v1GetFiles, "GET", "/v1/files", query, nil, nil)
		c.Assert(response.StatusCode, Equals, http.StatusOK)

		r := decodeResp(c, body, http.StatusOK, ResponseTypeSync)
		assertListResult(c, r.Result, 0, "file", tmpDir, "foo", "664", 1)
		assertListResult(c, r.Result, 1, "file", tmpDir, "one.txt", "600", 2)
		assertListResult(c, r.Result, 2, "directory", tmpDir, "sub", "775", -1)
		assertListResult(c, r.Result, 3, "file", tmpDir, "two.txt", "755", 3)
	}
}

func (s *filesSuite) TestListFilesDirItself(c *C) {
	tmpDir := createTestFiles(c)

	query := url.Values{
		"action":    []string{"list"},
		"pattern":   []string{tmpDir + "/sub"},
		"directory": []string{"true"},
	}
	response, body := doRequest(c, v1GetFiles, "GET", "/v1/files", query, nil, nil)
	c.Assert(response.StatusCode, Equals, http.StatusOK)

	r := decodeResp(c, body, http.StatusOK, ResponseTypeSync)
	assertListResult(c, r.Result, 0, "directory", tmpDir, "sub", "775", -1)
}

func (s *filesSuite) TestListFilesGlob(c *C) {
	tmpDir := createTestFiles(c)

	query := url.Values{
		"action":  []string{"list"},
		"pattern": []string{tmpDir + "/*.txt"},
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
		"action":  []string{"list"},
		"pattern": []string{tmpDir + "/foo"},
	}
	response, body := doRequest(c, v1GetFiles, "GET", "/v1/files", query, nil, nil)
	c.Assert(response.StatusCode, Equals, http.StatusOK)

	r := decodeResp(c, body, http.StatusOK, ResponseTypeSync)
	assertListResult(c, r.Result, 0, "file", tmpDir, "foo", "664", 1)
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
		"path:" + tmpDir + "/one.txt": "be",
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
		"path:" + tmpDir + "/foo":     "a",
		"path:" + tmpDir + "/one.txt": "be",
		"path:" + tmpDir + "/two.txt": "cee",
	})
}

func (s *filesSuite) TestReadErrors(c *C) {
	tmpDir := createTestFiles(c)
	writeTempFile(c, tmpDir, "no-access", "x", 0)

	query := url.Values{
		"action": []string{"read"},
		"path": []string{
			tmpDir + "/no-exist",
			tmpDir + "/foo",
			tmpDir + "/no-access",
			"relative-path",
		},
	}
	headers := http.Header{
		"Accept": []string{"multipart/form-data"},
	}
	response, body := doRequest(c, v1GetFiles, "GET", "/v1/files", query, headers, nil)
	c.Check(response.StatusCode, Equals, http.StatusBadRequest)

	var r testFilesResponse
	files := readMultipart(c, response, body, &r)
	c.Check(r.Type, Equals, "sync")
	c.Check(r.StatusCode, Equals, http.StatusBadRequest)
	c.Check(r.Status, Equals, "Bad Request")
	c.Check(r.Result, HasLen, 4)
	checkFileResult(c, r.Result[0], tmpDir+"/no-exist", "not-found", ".*: no such file or directory")
	checkFileResult(c, r.Result[1], tmpDir+"/foo", "", "")
	checkFileResult(c, r.Result[2], tmpDir+"/no-access", "permission-denied", ".*: permission denied")
	checkFileResult(c, r.Result[3], "relative-path", "generic-file-error", "paths must be absolute .*")

	c.Check(files, DeepEquals, map[string]string{
		"path:" + tmpDir + "/foo": "a",
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
			{Path: tmpDir + "/make/my/parents", MakeParents: true, Permissions: "755"},
		},
	}
	reqBody, err := json.Marshal(payload)
	c.Assert(err, IsNil)
	response, body := doRequest(c, v1PostFiles, "POST", "/v1/files", nil, headers, reqBody)
	c.Check(response.StatusCode, Equals, http.StatusBadRequest)

	var r testFilesResponse
	c.Assert(json.NewDecoder(body).Decode(&r), IsNil)
	c.Check(r.StatusCode, Equals, http.StatusBadRequest)
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
	c.Check(st.Mode().Perm(), Equals, os.FileMode(0o775))
	st, err = os.Stat(tmpDir + "/make/my/parents")
	c.Assert(err, IsNil)
	c.Check(st.Mode().Perm(), Equals, os.FileMode(0o755))
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
	c.Assert(os.Mkdir(tmpDir+"/empty", 0o775), IsNil)
	c.Assert(os.Mkdir(tmpDir+"/non-empty", 0o775), IsNil)
	writeTempFile(c, tmpDir, "non-empty/bar", "b", 0o644)
	c.Assert(os.Mkdir(tmpDir+"/recursive", 0o775), IsNil)
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
	c.Check(response.StatusCode, Equals, http.StatusBadRequest)

	var r testFilesResponse
	c.Assert(json.NewDecoder(body).Decode(&r), IsNil)
	c.Check(r.StatusCode, Equals, http.StatusBadRequest)
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
		"Content-Type": []string{"multipart/form-data; boundary=FOO"},
	}
	response, body := doRequest(c, v1PostFiles, "POST", "/v1/files", nil, headers, []byte{})
	c.Check(response.StatusCode, Equals, http.StatusBadRequest)
	assertError(c, body, http.StatusBadRequest, "", `cannot read request metadata: .*`)
}

func (s *filesSuite) TestWriteNoBoundary(c *C) {
	headers := http.Header{
		"Content-Type": []string{"multipart/form-data"},
	}
	response, body := doRequest(c, v1PostFiles, "POST", "/v1/files", nil, headers, []byte{})
	c.Check(response.StatusCode, Equals, http.StatusBadRequest)
	assertError(c, body, http.StatusBadRequest, "", `invalid boundary ""`)
}

func (s *filesSuite) TestWriteInvalidRequestField(c *C) {
	headers := http.Header{
		"Content-Type": []string{"multipart/form-data; boundary=BOUNDARY"},
	}
	response, body := doRequest(c, v1PostFiles, "POST", "/v1/files", nil, headers,
		[]byte(`
--BOUNDARY
Content-Disposition: form-data; name="foo"

{"foo": "bar"}
--BOUNDARY--
`))
	c.Check(response.StatusCode, Equals, http.StatusBadRequest)
	assertError(c, body, http.StatusBadRequest, "", `metadata field name must be "request", not "foo"`)
}

func (s *filesSuite) TestWriteInvalidFileField(c *C) {
	headers := http.Header{
		"Content-Type": []string{"multipart/form-data; boundary=BOUNDARY"},
	}
	response, body := doRequest(c, v1PostFiles, "POST", "/v1/files", nil, headers,
		[]byte(`
--BOUNDARY
Content-Disposition: form-data; name="request"

{"action": "write", "files": [
	{"path": "/foo/bar"}
]}
--BOUNDARY
Content-Disposition: form-data; name="bad"; filename="foo"

Bad file field
--BOUNDARY--
`))
	c.Check(response.StatusCode, Equals, http.StatusBadRequest)
	assertError(c, body, http.StatusBadRequest, "", `field name must be in format "file:/path".*`)
}

func (s *filesSuite) TestWriteNoMetadataForPath(c *C) {
	headers := http.Header{
		"Content-Type": []string{"multipart/form-data; boundary=BOUNDARY"},
	}
	response, body := doRequest(c, v1PostFiles, "POST", "/v1/files", nil, headers,
		[]byte(`
--BOUNDARY
Content-Disposition: form-data; name="request"

{"action": "write", "files": [
	{"path": "/foo/bar"}
]}
--BOUNDARY
Content-Disposition: form-data; name="file:/no-metadata"; filename="foo"

No metadata
--BOUNDARY--
`))
	c.Check(response.StatusCode, Equals, http.StatusBadRequest)
	assertError(c, body, http.StatusBadRequest, "", `no metadata for path "/no-metadata"`)
}

func (s *filesSuite) TestWriteInvalidMetadata(c *C) {
	headers := http.Header{
		"Content-Type": []string{"multipart/form-data; boundary=BOUNDARY"},
	}
	response, body := doRequest(c, v1PostFiles, "POST", "/v1/files", nil, headers,
		[]byte(`
--BOUNDARY
Content-Disposition: form-data; name="request"

not json
--BOUNDARY--
`))
	c.Check(response.StatusCode, Equals, http.StatusBadRequest)
	assertError(c, body, http.StatusBadRequest, "", `cannot decode request metadata.*`)

	response, body = doRequest(c, v1PostFiles, "POST", "/v1/files", nil, headers,
		[]byte(`
--BOUNDARY
Content-Disposition: form-data; name="request"

{"action": "foo"}
--BOUNDARY--
`))
	c.Check(response.StatusCode, Equals, http.StatusBadRequest)
	assertError(c, body, http.StatusBadRequest, "", `multipart action must be "write", not "foo"`)
}

func (s *filesSuite) TestWriteNoFiles(c *C) {
	headers := http.Header{
		"Content-Type": []string{"multipart/form-data; boundary=BOUNDARY"},
	}
	response, body := doRequest(c, v1PostFiles, "POST", "/v1/files", nil, headers,
		[]byte(`
--BOUNDARY
Content-Disposition: form-data; name="request"

{"action": "write"}
--BOUNDARY--
`))
	c.Check(response.StatusCode, Equals, http.StatusBadRequest)
	assertError(c, body, http.StatusBadRequest, "", "must specify one or more files")
}

func (s *filesSuite) TestWriteSingle(c *C) {
	tmpDir := c.MkDir()
	path := tmpDir + "/hello.txt"

	headers := http.Header{
		"Content-Type": []string{"multipart/form-data; boundary=BOUNDARY"},
	}
	response, body := doRequest(c, v1PostFiles, "POST", "/v1/files", nil, headers,
		[]byte(fmt.Sprintf(`
--BOUNDARY
Content-Disposition: form-data; name="request"

{"action": "write", "files": [
	{"path": "%[1]s"}
]}
--BOUNDARY
Content-Disposition: form-data; name="file:%[1]s"; filename="hello.txt"

Hello world
--BOUNDARY--
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

func (s *filesSuite) TestWriteMultiple(c *C) {
	tmpDir := c.MkDir()
	path0 := tmpDir + "/hello.txt"
	path1 := tmpDir + "/byebye.txt"
	path2 := tmpDir + "/foo/bar.txt"

	headers := http.Header{
		"Content-Type": []string{"multipart/form-data; boundary=BOUNDARY"},
	}
	response, body := doRequest(c, v1PostFiles, "POST", "/v1/files", nil, headers,
		[]byte(fmt.Sprintf(`
--BOUNDARY
Content-Disposition: form-data; name="request"

{
	"action": "write",
	"files": [
		{"path": "%[1]s"},
		{"path": "%[2]s", "permissions": "600"},
		{"path": "%[3]s", "make-dirs": true}
	]
}
--BOUNDARY
Content-Disposition: form-data; name="file:%[1]s"; filename="hello.txt"

Hello
--BOUNDARY
Content-Disposition: form-data; name="file:%[2]s"; filename="byebye.txt"

Bye bye
--BOUNDARY
Content-Disposition: form-data; name="file:%[3]s"; filename="bar.txt"

Foo
Bar
--BOUNDARY--
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
	assertFile(c, path1, 0o600, "Bye bye")
	assertFile(c, path2, 0o644, "Foo\nBar")
	info, err := os.Stat(tmpDir + "/foo")
	c.Assert(err, IsNil)
	c.Assert(info.Mode().Perm(), Equals, os.FileMode(0o775))
}

func (s *filesSuite) TestWriteErrors(c *C) {
	tmpDir := c.MkDir()
	pathNoContent := tmpDir + "/no-content"
	pathNotAbsolute := "path-not-absolute"
	pathNotFound := tmpDir + "/not-found/foo"
	pathPermissionDenied := "/dev/permission-denied"

	headers := http.Header{
		"Content-Type": []string{"multipart/form-data; boundary=BOUNDARY"},
	}
	response, body := doRequest(c, v1PostFiles, "POST", "/v1/files", nil, headers,
		[]byte(fmt.Sprintf(`
--BOUNDARY
Content-Disposition: form-data; name="request"

{
	"action": "write",
	"files": [
		{"path": "%[1]s"},
		{"path": "%[2]s"},
		{"path": "%[3]s"},
		{"path": "%[4]s"}
	]
}
--BOUNDARY
Content-Disposition: form-data; name="file:%[2]s"; filename="a"

path not absolute
--BOUNDARY
Content-Disposition: form-data; name="file:%[3]s"; filename="b"

dir not found
--BOUNDARY
Content-Disposition: form-data; name="file:%[4]s"; filename="c"

permission denied
--BOUNDARY--
`, pathNoContent, pathNotAbsolute, pathNotFound, pathPermissionDenied)))
	c.Check(response.StatusCode, Equals, http.StatusBadRequest)

	var r testFilesResponse
	c.Assert(json.NewDecoder(body).Decode(&r), IsNil)
	c.Check(r.StatusCode, Equals, http.StatusBadRequest)
	c.Check(r.Type, Equals, "sync")
	c.Check(r.Result, HasLen, 4)
	checkFileResult(c, r.Result[0], pathNoContent, "generic-file-error", "no file content for path.*")
	checkFileResult(c, r.Result[1], pathNotAbsolute, "generic-file-error", "paths must be absolute.*")
	checkFileResult(c, r.Result[2], pathNotFound, "not-found", ".*")
	checkFileResult(c, r.Result[3], pathPermissionDenied, "permission-denied", ".*")

	c.Check(osutil.CanStat(pathNoContent), Equals, false)
	c.Check(osutil.CanStat(pathNotAbsolute), Equals, false)
	c.Check(osutil.CanStat(pathNotFound), Equals, false)
	c.Check(osutil.CanStat(pathPermissionDenied), Equals, false)
}

func assertFile(c *C, path string, perm os.FileMode, content string) {
	b, err := ioutil.ReadFile(path)
	c.Assert(err, IsNil)
	c.Assert(string(b), Equals, content)
	info, err := os.Stat(path)
	c.Assert(err, IsNil)
	c.Assert(info.Mode().Perm(), Equals, perm)
}

// Read a multipart HTTP response body, parse JSON in "response" field to result,
// and return map of file field to file content.
func readMultipart(c *C, response *http.Response, body io.Reader, result interface{}) map[string]string {
	contentType := response.Header.Get("Content-Type")
	mediaType, params, err := mime.ParseMediaType(contentType)
	c.Assert(err, IsNil)
	c.Assert(mediaType, Equals, "multipart/form-data")
	c.Assert(params["boundary"], Not(Equals), "")
	mr := multipart.NewReader(body, params["boundary"])
	form, err := mr.ReadForm(4096)
	c.Assert(err, IsNil)

	err = json.Unmarshal([]byte(form.Value["response"][0]), result)
	c.Assert(err, IsNil)

	files := make(map[string]string)
	for p, fhs := range form.File {
		for _, fh := range fhs {
			f, err := fh.Open()
			c.Assert(err, IsNil)
			b, err := ioutil.ReadAll(f)
			c.Assert(err, IsNil)
			f.Close()
			files[p] = string(b)
		}
	}
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
	result := r.Result.(map[string]interface{})
	if kind != "" {
		c.Assert(result["kind"], Equals, kind)
	}
	c.Assert(result["message"], Matches, message)
}

func createTestFiles(c *C) string {
	tmpDir := c.MkDir()
	writeTempFile(c, tmpDir, "foo", "a", 0o664)
	writeTempFile(c, tmpDir, "one.txt", "be", 0o600)
	c.Assert(os.Mkdir(tmpDir+"/sub", 0o775), IsNil)
	writeTempFile(c, tmpDir, "two.txt", "cee", 0o755)
	return tmpDir
}

func writeTempFile(c *C, dir, filename, content string, perm os.FileMode) {
	err := ioutil.WriteFile(filepath.Join(dir, filename), []byte(content), perm)
	c.Assert(err, IsNil)
}

func assertListResult(c *C, result interface{}, index int, typ, dir, name, perms string, size int) {
	x := result.([]interface{})[index].(map[string]interface{})
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
}

func decodeResp(c *C, body io.Reader, status int, typ ResponseType) respJSON {
	var r respJSON
	c.Assert(json.NewDecoder(body).Decode(&r), IsNil)
	c.Assert(r.Status, Equals, status)
	c.Assert(r.StatusText, Equals, http.StatusText(status))
	c.Assert(r.Type, Equals, typ)
	return r
}
