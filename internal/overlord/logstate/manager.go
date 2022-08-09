package logstate

import (
	"fmt"
	"io/ioutil"
	"sync"

	"github.com/canonical/pebble/internal/logger"
	"github.com/canonical/pebble/internal/plan"
)

type PlanWatcher interface {
	PlanChanged(p *plan.Plan)
}

type LogManager struct {
	mutex        sync.Mutex
	destinations map[string]*SyslogTransport
	labels       map[string]string
	watchers     []PlanWatcher
}

func NewLogManager() *LogManager {
	return &LogManager{destinations: make(map[string]*SyslogTransport)}
}

func (m *LogManager) Notify(w PlanWatcher) { m.watchers = append(m.watchers, w) }

func (m *LogManager) GetTransport(destination string) (*SyslogTransport, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	dest, ok := m.destinations[destination]
	if !ok {
		return nil, fmt.Errorf("invalid service logging destination %q", destination)
	}
	return dest, nil
}

// PlanChanged handles updates to the plan (server configuration),
// stopping the previous checks and starting the new ones as required.
func (m *LogManager) PlanChanged(p *plan.Plan) {
	if len(p.LogDestinations) == 0 && len(p.LogLabels) == 0 {
		return
	}

	m.mutex.Lock()
	defer m.mutex.Unlock()

	needNotify := false
	defer func() {
		if needNotify {
			for _, w := range m.watchers {
				w.PlanChanged(p)
			}
		}
	}()

	// update destinations
	for name, dest := range p.LogDestinations {
		if dest.Type != "syslog" {
			logger.Noticef("unsupported logging destination type: %v", dest.Type)
			continue
		}
		var caData []byte
		var err error
		if dest.TLS != nil {
			caData, err = ioutil.ReadFile(dest.TLS.CAfile)
			if err != nil {
				logger.Noticef("could not read CA file \"%s\"", dest.TLS.CAfile)
				continue
			}
		}

		needNotify = true

		orig, ok := m.destinations[name]
		if ok {
			orig.Update(dest.Protocol, dest.Address, caData)
		} else {
			m.destinations[name] = NewSyslogTransport(dest.Protocol, dest.Address, caData)
		}
	}

	// update labels
	for key, val := range p.LogLabels {
		orig, ok := m.labels[key]
		needNotify = needNotify || !ok || orig != val
		m.labels[key] = val
	}
}
