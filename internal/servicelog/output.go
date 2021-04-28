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
// along with this program.  If not, see <http://www.gnu.org/licenses/

package servicelog

import (
	"io"
	"time"
)

// Output of logs sourced from an iterator using Sink.
type Output interface {
	WriteLog(timestamp time.Time, serviceName string, stream StreamID, message io.Reader) error
}

type OutputFunc func(time.Time, string, StreamID, io.Reader) error

func (f OutputFunc) WriteLog(timestamp time.Time, serviceName string, stream StreamID, message io.Reader) error {
	return f(timestamp, serviceName, stream, message)
}

// Sink logs from the iterator to Output labeled with the service name.
// Sink blocks until done is closed.
func Sink(it Iterator, out Output, serviceName string, done <-chan struct{}) error {
	last := false
	for {
		more := it.More()
		for it.Next() {
			err := out.WriteLog(it.Timestamp(), serviceName, it.StreamID(), it)
			if err != nil {
				return err
			}
		}
		if last {
			break
		}
		select {
		case <-more:
		case <-done:
			last = true
		}
	}
	return nil
}
