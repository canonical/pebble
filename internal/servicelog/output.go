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
	WriteLog(timestamp time.Time, serviceName string, stream StreamID, length int, message io.Reader) error
}

type OutputFunc func(timestamp time.Time, serviceName string, stream StreamID, length int, message io.Reader) error

func (f OutputFunc) WriteLog(timestamp time.Time, serviceName string, stream StreamID, length int, message io.Reader) error {
	return f(timestamp, serviceName, stream, length, message)
}

// Sink logs from the iterator to Output labeled with the service name.
// Sink blocks until done is closed.
func Sink(it Iterator, out Output, serviceName string, done <-chan struct{}) error {
	for it.Next(done) {
		err := out.WriteLog(it.Timestamp(), serviceName, it.StreamID(), it.Length(), it)
		if err != nil {
			return err
		}
	}
	return nil
}
