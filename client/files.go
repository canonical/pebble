// Copyright (c) 2022 Canonical Ltd
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

package client

import (
	"bufio"
	"encoding/json"
	"io"
	"mime/multipart"
	"os"
)

// PushFileOptions contains the options for a call to PushFile.
type PushFileOptions struct {
	// LocalPath indicates the path to the file that will be pushed (required).
	LocalPath string

	// RemotePath indicates the absolute path of the file in the destination
	// machine (required).
	RemotePath string

	// MakeDirs, if true, will create any non-existing directories in the path
	// to the remote file. If false, the default, the call to PushFile will
	// fail if any non-existing directory is found on the remote path.
	MakeDirs bool

	// Permissions indicates the mode of the file in the destination machine.
	// Defaults to 0644. Note that, when used together with MakeDirs, the
	// directories that might be created will not use this mode, but 0755.
	Permissions string

	// UserID indicates the user ID of the owner for the file in the destination
	// machine. When used together with MakeDirs, the directories that might be
	// created will also be owned by this user.
	UserID *int

	// User indicates the name of the owner user for the file in the destination
	// machine. When used together with MakeDirs, the directories that might be
	// created will also be owned by this user.
	User string

	// GroupID indicates the ID of the owner group for the file in the
	// destination machine. When used together with MakeDirs, the directories
	// that might be created will also be owned by this group.
	GroupID *int

	// Group indicates the name of the owner group for the file in the
	// destination machine. When used together with MakeDirs, the directories
	// that might be created will also be owned by this group.
	Group string
}

type writeFilesPayload struct {
	Action string           `json:"action"`
	Files  []writeFilesItem `json:"files"`
}

type writeFilesItem struct {
	Path        string `json:"path"`
	MakeDirs    bool   `json:"make-dirs"`
	Permissions string `json:"permissions"`
	UserID      *int   `json:"user-id"`
	User        string `json:"user"`
	GroupID     *int   `json:"group-id"`
	Group       string `json:"group"`
}

type fileResult struct {
	Path  string       `json:"path"`
	Error *errorResult `json:"error,omitempty"`
}

type errorResult struct {
	Message string      `json:"message"`
	Kind    string      `json:"kind,omitempty"`
	Value   interface{} `json:"value,omitempty"`
}

type fileUpload struct {
	localPath string
	info      writeFilesItem
}

type fileUploader struct {
	pr *io.PipeReader
	pw *io.PipeWriter

	r *bufio.Reader
	w *bufio.Writer

	mw    *multipart.Writer
	files *[]fileUpload
}

func newFileUploader() (*fileUploader, error) {
	u := &fileUploader{}
	u.pr, u.pw = io.Pipe()
	u.r = bufio.NewReader(u.pr)
	u.w = bufio.NewWriter(u.pw)
	u.mw = multipart.NewWriter(u.w)

	return u, nil
}

func (u *fileUploader) prepareUpload(files []fileUpload) error {
	// Create metadata form field
	metaWriter, err := u.mw.CreateFormField("request")
	if err != nil {
		return err
	}

	// Build and encode JSON payload
	payload := writeFilesPayload{
		Action: "write",
		Files:  make([]writeFilesItem, len(files)),
	}
	for i, file := range files {
		payload.Files[i] = file.info
	}

	encoder := json.NewEncoder(metaWriter)
	if err := encoder.Encode(&payload); err != nil {
		return err
	}

	u.files = &files
	return nil
}

func (u *fileUploader) uploadFiles() error {
	for _, file := range *u.files {
		w, err := u.mw.CreateFormFile("files", file.info.Path)
		if err != nil {
			return err
		}

		r, err := os.Open(file.localPath)
		if err != nil {
			return err
		}
		defer r.Close()

		if _, err := io.Copy(w, r); err != nil {
			return err
		}
	}
	return u.mw.Close()
}

// PushFile sends a local file to the remote machine.
func (client *Client) PushFile(opts *PushFileOptions) error {
	u, err := newFileUploader()
	if err != nil {
		return err
	}

	err = u.prepareUpload([]fileUpload{
		{
			localPath: opts.LocalPath,
			info: writeFilesItem{
				Path:        opts.RemotePath,
				MakeDirs:    opts.MakeDirs,
				Permissions: opts.Permissions,
				UserID:      opts.UserID,
				User:        opts.User,
				GroupID:     opts.GroupID,
				Group:       opts.Group,
			},
		},
	})
	if err != nil {
		return err
	}

	go func() {
		if err := u.uploadFiles(); err != nil {
			panic(err)
		}
		u.w.Flush()
		u.pw.Close()
	}()

	var result []fileResult
	_, err = client.doSync("POST", "/v1/files", nil, map[string]string{
		"Content-Type": u.mw.FormDataContentType(),
	}, u.r, &result)
	if err != nil {
		return err
	}

	return nil
}
