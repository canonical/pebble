// Copyright (c) 2023 Canonical Ltd
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

package logstate

import (
	"sync"

	"github.com/canonical/pebble/internals/logger"
	"github.com/canonical/pebble/internals/servicelog"
	"gopkg.in/tomb.v2"
)

// logPuller handles pulling logs from a single iterator and sending to the
// main control loop.
type logPuller struct {
	iterator servicelog.Iterator
	entryCh  chan<- servicelog.Entry

	tomb tomb.Tomb
}

// loop pulls logs off the iterator and sends them on the entryCh.
// The loop will terminate:
//   - if the puller's context is cancelled
//   - once the ringbuffer is closed and the iterator finishes reading all
//     remaining logs.
func (p *logPuller) loop() error {
	defer func() { _ = p.iterator.Close() }()

	parser := servicelog.NewParser(p.iterator, parserSize)
	for p.iterator.Next(p.tomb.Dying()) {
		for parser.Next() {
			if err := parser.Err(); err != nil {
				return err
			}

			select {
			case p.entryCh <- parser.Entry():
			case <-p.tomb.Dying():
				return nil
			}
		}
	}
	return nil
}

// pullerGroup represents a group of logPullers, and provides methods for a
// gatherer to manage logPullers (dynamically add/remove, kill all, wait for
// all to finish).
type pullerGroup struct {
	targetName string

	// Currently active logPullers, indexed by service name
	pullers map[string]*logPuller
	// Mutex for pullers map
	mu sync.RWMutex
}

func newPullerGroup(targetName string) *pullerGroup {
	pg := &pullerGroup{
		targetName: targetName,
		pullers:    map[string]*logPuller{},
	}
	return pg
}

func (pg *pullerGroup) Add(serviceName string, buffer *servicelog.RingBuffer, entryCh chan<- servicelog.Entry) {
	lp := &logPuller{
		iterator: buffer.TailIterator(),
		entryCh:  entryCh,
	}
	lp.tomb.Go(func() error {
		err := lp.loop()
		// Once the puller exits, we should remove it from the group.
		//pg.Remove(serviceName) - this seems to cause deadlock though
		return err
	})

	// There shouldn't already be a puller for this service, but if there is,
	// shut it down first and wait for it to die.
	pg.Remove(serviceName)

	pg.mu.Lock()
	defer pg.mu.Unlock()
	pg.pullers[serviceName] = lp
}

// List returns a list of all service names for which we have a currently
// active puller.
func (pg *pullerGroup) List() []string {
	pg.mu.RLock()
	defer pg.mu.RUnlock()

	var svcs []string
	for svc := range pg.pullers {
		svcs = append(svcs, svc)
	}
	return svcs
}

func (pg *pullerGroup) Remove(serviceName string) {
	pg.mu.RLock()
	puller, pullerExists := pg.pullers[serviceName]
	pg.mu.RUnlock()

	if !pullerExists {
		return
	}

	puller.tomb.Kill(nil)
	err := puller.tomb.Wait()
	if err != nil {
		logger.Noticef("Error from log puller: %v", err)
	}

	pg.mu.Lock()
	defer pg.mu.Unlock()
	delete(pg.pullers, serviceName)
}

func (pg *pullerGroup) KillAll() {
	pg.mu.RLock()
	defer pg.mu.RUnlock()

	for _, puller := range pg.pullers {
		puller.tomb.Kill(nil)
	}
}

// Done returns a channel which can be waited on until all pullers have finished.
func (pg *pullerGroup) Done() <-chan struct{} {
	pg.mu.RLock()
	pullers := pg.pullers
	pg.mu.RUnlock()

	done := make(chan struct{})
	go func() {
		for _, puller := range pullers {
			err := puller.tomb.Wait()
			if err != nil {
				logger.Noticef("Error from log puller: %v", err)
			}
		}
		close(done)
	}()

	return done
}

func (pg *pullerGroup) Contains(serviceName string) bool {
	pg.mu.RLock()
	defer pg.mu.RUnlock()
	_, ok := pg.pullers[serviceName]
	return ok
}

func (pg *pullerGroup) Len() int {
	pg.mu.RLock()
	defer pg.mu.RUnlock()
	return len(pg.pullers)
}
