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
	"crypto/x509"
	"fmt"
)

var (
	ParseErrorInTest       = parseError
	CalculateFileMode      = calculateFileMode
	GetIdentityFingerprint = getIdentityFingerprint
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

// SysInfoWithServerID is a modified version of SysInfo for testing if
// we get a valid server ID certificate returned while using HTTPS.
func (client *Client) SysInfoWithServerID() (*x509.Certificate, *SysInfo, error) {
	var sysInfo SysInfo

	resp, err := client.Requester().Do(context.Background(), &RequestOptions{
		Type:   SyncRequest,
		Method: "GET",
		Path:   "/v1/system-info",
	})
	if err != nil {
		return nil, nil, fmt.Errorf("cannot obtain system details: %w", err)
	}
	err = resp.DecodeResult(&sysInfo)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot obtain system details: %w", err)
	}

	return resp.TLSServerIDCert, &sysInfo, nil
}
