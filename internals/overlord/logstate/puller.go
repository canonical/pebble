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
	"context"
	"sync"

	"github.com/canonical/pebble/internals/servicelog"
)

// logPuller handles pulling logs from a single iterator and sending to the
// main control loop.
type logPuller struct {
	iterator servicelog.Iterator
	entryCh  chan<- servicelog.Entry

	ctx  context.Context
	kill context.CancelFunc
}

func (p *logPuller) loop() {
	defer func() { _ = p.iterator.Close() }()

	parser := servicelog.NewParser(p.iterator, parserSize)
	for p.iterator.Next(p.ctx.Done()) {
		for parser.Next() {
			if err := parser.Err(); err != nil {
				return
			}

			// Check if our context has been cancelled
			select {
			case <-p.ctx.Done():
				return
			default:
			}

			select {
			case p.entryCh <- parser.Entry():
			case <-p.ctx.Done():
				return
			}
		}
	}
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
	// Common context for all pullers. Each puller uses a derived context so we
	// can easily kill all pullers (if required) during teardown.
	ctx context.Context
	// Cancel func for ctx
	kill context.CancelFunc
	// WaitGroup for pullers - we use this during teardown to know when all the
	// pullers are finished.
	wg sync.WaitGroup
}

func newPullerGroup(targetName string) *pullerGroup {
	pg := &pullerGroup{
		targetName: targetName,
		pullers:    map[string]*logPuller{},
	}
	pg.ctx, pg.kill = context.WithCancel(context.Background())
	return pg
}

func (pg *pullerGroup) Add(serviceName string, buffer *servicelog.RingBuffer, entryCh chan<- servicelog.Entry) {
	lp := &logPuller{
		iterator: buffer.TailIterator(),
		entryCh:  entryCh,
	}
	lp.ctx, lp.kill = context.WithCancel(pg.ctx)

	pg.wg.Add(1) // this will be marked as done once loop finishes
	go func() {
		lp.loop()
		pg.wg.Done()
		// TODO: remove puller from map ?
	}()

	pg.mu.Lock()
	defer pg.mu.Unlock()
	if puller, ok := pg.pullers[serviceName]; ok {
		// This should never happen, but just in case, shut down the old puller.
		puller.kill()
	}
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
	pg.mu.Lock()
	defer pg.mu.Unlock()

	if puller, ok := pg.pullers[serviceName]; ok {
		puller.kill()
		delete(pg.pullers, serviceName)
	}

}

func (pg *pullerGroup) KillAll() {
	pg.kill()
}

// Done returns a channel which can be waited on until all pullers have finished.
func (pg *pullerGroup) Done() <-chan struct{} {
	done := make(chan struct{})
	go func() {
		pg.wg.Wait()
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
