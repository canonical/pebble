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
	"context"
	"fmt"
	"io"
	"net/url"
)

var (
	ParseErrorInTest  = parseError
	CalculateFileMode = calculateFileMode
)

func (client *Client) SetDoer(d doer) {
	client.Requester().(*defaultRequester).doer = d
}

// TODO: Clean up tests to use the new Requester API. Tests do not generate a client.response type
// reply in the body while SyncRequest or AsyncRequest responses assume the JSON body can be
// unmarshalled into client.response.
func (client *Client) Do(method, path string, query url.Values, body io.Reader, v interface{}) error {
	resp, err := client.Requester().Do(context.Background(), &RequestOptions{
		Type:    RawRequest,
		Method:  method,
		Path:    path,
		Query:   query,
		Headers: nil,
		Body:    body,
	})
	if err != nil {
		return err
	}
	err = decodeInto(resp.Body, v)
	if err != nil {
		return err
	}
	return nil
}

func (client *Client) FakeAsyncRequest() (changeId string, err error) {
	resp, err := client.doAsync("GET", "/v1/async-test", nil, nil, nil, nil)
	if err != nil {
		return "", fmt.Errorf("cannot do async test: %v", err)
	}
	return resp.ChangeID, nil
}

func (client *Client) SetGetWebsocket(f getWebsocketFunc) {
	client.getWebsocket = f
}

// WaitStdinDone waits for WebsocketSendStream to be finished calling
// WriteMessage to avoid a race condition.
func (p *ExecProcess) WaitStdinDone() {
	<-p.stdinDone
}

type ClientWebsocket = clientWebsocket
