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

func (s *filesSuite) TestReadFilesNoPaths(c *C) {
	query := url.Values{"action": []string{"read"}}
	response, body := doRequest(c, v1GetFiles, "GET", "/v1/files", query, nil, nil)
	c.Assert(response.StatusCode, Equals, http.StatusBadRequest)
	assertError(c, body, http.StatusBadRequest, "", "must specify one or more paths")
}

func (s *filesSuite) TestReadFilesNoMultipartHeader(c *C) {
	query := url.Values{"action": []string{"read"}, "path": []string{"foo"}}
	response, body := doRequest(c, v1GetFiles, "GET", "/v1/files", query, nil, nil)
	c.Assert(response.StatusCode, Equals, http.StatusBadRequest)
	assertError(c, body, http.StatusBadRequest, "", "must accept multipart/form-data")
}

type testReadFilesResponse struct {
	Type       string
	StatusCode int `json:"status-code"`
	Status     string
	Result     []struct {
		Path  string
		Error struct {
			Kind    string
			Message string
		}
	}
}

func (s *filesSuite) TestReadFilesSingle(c *C) {
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

	var r testReadFilesResponse
	files := readMultipart(c, response, body, &r)
	c.Check(r.Type, Equals, "sync")
	c.Check(r.StatusCode, Equals, http.StatusOK)
	c.Check(r.Status, Equals, "OK")
	c.Check(r.Result, HasLen, 1)
	c.Check(r.Result[0].Path, Equals, tmpDir+"/one.txt")
	c.Check(r.Result[0].Error.Kind, Equals, "")

	c.Check(files, DeepEquals, map[string]string{
		"path:" + tmpDir + "/one.txt": "be",
	})
}

func (s *filesSuite) TestReadFilesMultiple(c *C) {
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

	var r testReadFilesResponse
	files := readMultipart(c, response, body, &r)
	c.Check(r.Type, Equals, "sync")
	c.Check(r.StatusCode, Equals, http.StatusOK)
	c.Check(r.Status, Equals, "OK")
	c.Check(r.Result, HasLen, 3)
	c.Check(r.Result[0].Path, Equals, tmpDir+"/foo")
	c.Check(r.Result[0].Error.Kind, Equals, "")
	c.Check(r.Result[1].Path, Equals, tmpDir+"/one.txt")
	c.Check(r.Result[1].Error.Kind, Equals, "")
	c.Check(r.Result[2].Path, Equals, tmpDir+"/two.txt")
	c.Check(r.Result[2].Error.Kind, Equals, "")

	c.Check(files, DeepEquals, map[string]string{
		"path:" + tmpDir + "/foo":     "a",
		"path:" + tmpDir + "/one.txt": "be",
		"path:" + tmpDir + "/two.txt": "cee",
	})
}

func (s *filesSuite) TestReadFilesErrors(c *C) {
	tmpDir := createTestFiles(c)
	writeTempFile(c, tmpDir, "no-access", "x", 0)

	query := url.Values{
		"action": []string{"read"},
		"path":   []string{tmpDir + "/no-exist", tmpDir + "/foo", tmpDir + "/no-access"},
	}
	headers := http.Header{
		"Accept": []string{"multipart/form-data"},
	}
	response, body := doRequest(c, v1GetFiles, "GET", "/v1/files", query, headers, nil)
	c.Check(response.StatusCode, Equals, http.StatusBadRequest)

	var r testReadFilesResponse
	files := readMultipart(c, response, body, &r)
	c.Check(r.Type, Equals, "error")
	c.Check(r.StatusCode, Equals, http.StatusBadRequest)
	c.Check(r.Status, Equals, "Bad Request")
	c.Check(r.Result, HasLen, 3)
	c.Check(r.Result[0].Path, Equals, tmpDir+"/no-exist")
	c.Check(r.Result[0].Error.Kind, Equals, "not-found")
	c.Check(r.Result[0].Error.Message, Matches, ".*: no such file or directory")
	c.Check(r.Result[1].Path, Equals, tmpDir+"/foo")
	c.Check(r.Result[1].Error.Kind, Equals, "")
	c.Check(r.Result[2].Path, Equals, tmpDir+"/no-access")
	c.Check(r.Result[2].Error.Kind, Equals, "permission-denied")
	c.Check(r.Result[2].Error.Message, Matches, ".*: permission denied")

	c.Check(files, DeepEquals, map[string]string{
		"path:" + tmpDir + "/foo": "a",
	})
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
	c.Assert(os.Mkdir(filepath.Join(tmpDir, "sub"), 0o775), IsNil)
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
