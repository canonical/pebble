// Copyright (c) 2014-2020 Canonical Ltd
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
	"fmt"
	"io"
	"net/url"
)

var ParseErrorInTest = parseError

func (client *Client) SetDoer(d doer) {
	client.doer = d
}

func (client *Client) Do(method, path string, query url.Values, body io.Reader, v interface{}) error {
	return client.do(method, path, query, nil, body, v)
}

func (client *Client) FakeAsyncRequest() (changeId string, err error) {
	changeId, err = client.doAsync("GET", "/v1/async-test", nil, nil, nil)
	if err != nil {
		return "", fmt.Errorf("cannot do async test: %v", err)
	}
	return changeId, nil
}

func (client *Client) SetGetWebsocket(f getWebsocketFunc) {
	client.getWebsocket = f
}

func (p *ExecProcess) SetControlConn(c jsonWriter) {
	p.controlConn = c
}

// WaitStdinDone waits for WebsocketSendStream to be finished calling
// WriteMessage to avoid a race condition.
func (p *ExecProcess) WaitStdinDone() {
	<-p.stdinDone
}

type ClientWebsocket = clientWebsocket
