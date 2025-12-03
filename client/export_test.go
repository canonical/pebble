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
)

var (
	ParseErrorInTest  = parseError
	CalculateFileMode = calculateFileMode
)

func (client *Client) SetDoer(d doer) {
	client.Requester().(*defaultRequester).doer = d
}

func (client *Client) FakeAsyncRequest() (changeId string, err error) {
	resp, err := client.Requester().Do(context.Background(), &RequestOptions{
		Type:   AsyncRequest,
		Method: "GET",
		Path:   "/v1/async-test",
	})
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
