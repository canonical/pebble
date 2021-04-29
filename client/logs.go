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

package client

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"time"

	"github.com/canonical/pebble/internal/servicelog"
)

// Each log write is output as <metadata json> <newline> <message bytes>,
// for example (the "length" field excludes the first newline):
//
// {"time":"2021-04-23T01:28:52.660695091Z","service":"redis","stream":"stdout","length":10}
// message 9
// {"time":"2021-04-23T01:28:52.798839551Z","service":"thing","stream":"stdout","length":11}
// message 10
type logMeta struct {
	Time    time.Time `json:"time"`
	Service string    `json:"service"`
	Stream  string    `json:"stream"`
	Length  int       `json:"length"`
}

// Logs of the specified services passed to the provided output or all services
// when none are specified.
func (client *Client) Logs(services []string, output servicelog.Output) error {
	query := url.Values{}
	query.Set("follow", "false")
	for _, service := range services {
		query.Add("services", service)
	}
	headers := map[string]string{}
	res, err := client.raw("GET", "/v1/logs", query, headers, nil)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	reader := bufio.NewReader(res.Body)
	eofCheckBuffer := make([]byte, 1)
	for {
		meta := logMeta{}
		encoded, err := reader.ReadBytes('\n')
		if err == io.EOF {
			return nil
		} else if err != nil {
			return err
		}
		err = json.Unmarshal(encoded, &meta)
		if err != nil {
			return err
		}
		stream := servicelog.Unknown
		switch meta.Stream {
		case servicelog.Stdout.String():
			stream = servicelog.Stdout
		case servicelog.Stderr.String():
			stream = servicelog.Stderr
		}
		lr := io.LimitReader(reader, int64(meta.Length))
		err = output.WriteLog(meta.Time, meta.Service, stream, lr)
		if err != nil {
			return err
		}
		// Check we get EOF, otherwise the call to output.WriteLog didn't read all the bytes.
		_, err = lr.Read(eofCheckBuffer)
		if err == nil {
			return fmt.Errorf("malformed log reader")
		} else if err != nil && err != io.EOF {
			return err
		}
	}
}
