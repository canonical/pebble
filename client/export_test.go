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
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"time"
)

var (
	ParseErrorInTest  = parseError
	CalculateFileMode = calculateFileMode
)

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

// DebugGet sends a GET debug action to the server with the provided parameters.
func (client *Client) DebugGet(action string, result interface{}, params map[string]string) error {
	urlParams := url.Values{"action": []string{action}}
	for k, v := range params {
		urlParams.Set(k, v)
	}
	_, err := client.doSync("GET", "/v1/debug", urlParams, nil, nil, &result)
	return err
}

type debugAction struct {
	Action string      `json:"action"`
	Params interface{} `json:"params,omitempty"`
}

// DebugPost sends a POST debug action to the server with the provided parameters.
func (client *Client) DebugPost(action string, params interface{}, result interface{}) error {
	body, err := json.Marshal(debugAction{
		Action: action,
		Params: params,
	})
	if err != nil {
		return err
	}

	_, err = client.doSync("POST", "/v1/debug", nil, nil, bytes.NewReader(body), result)
	return err
}

// WaitStdinDone waits for WebsocketSendStream to be finished calling
// WriteMessage to avoid a race condition.
func (p *ExecProcess) WaitStdinDone() {
	<-p.stdinDone
}

type ClientWebsocket = clientWebsocket

// FakeDoRetry fakes the delays used by the do retry loop.
func FakeDoRetry(retry, timeout time.Duration) (restore func()) {
	oldRetry := doRetry
	oldTimeout := doTimeout
	doRetry = retry
	doTimeout = timeout
	return func() {
		doRetry = oldRetry
		doTimeout = oldTimeout
	}
}
