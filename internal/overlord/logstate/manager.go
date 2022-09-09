package logstate

import (
	"fmt"
	"sync"

	"github.com/canonical/pebble/internal/logger"
	"github.com/canonical/pebble/internal/plan"
)

type LogManager struct {
	mutex        sync.Mutex
	destinations map[string]*LogDestination
}

func NewLogManager() *LogManager {
	return &LogManager{destinations: make(map[string]*LogDestination)}
}

func (m *LogManager) GetDestination(destination string) (*LogDestination, error) {
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
	if len(p.LogDestinations) == 0 {
		return
	}

	m.mutex.Lock()
	defer m.mutex.Unlock()

	// update destinations
	for name, dest := range p.LogDestinations {
		var b LogBackend
		var err error
		switch dest.Type {
		case "loki":
			b, err = NewLokiBackend(dest.Address)
		case "syslog":
			b, err = NewSyslogBackend(dest.Address)
		default:
			logger.Noticef("unsupported logging destination type: %v", dest.Type)
			continue
		}
		if err != nil {
			logger.Noticef("invalid config for log destination %q: %v", name, err)
			continue
		}

		orig, ok := m.destinations[name]
		if ok {
			logger.Noticef("manager setting backend for log destination")
			orig.SetBackend(b)
		} else {
			logger.Noticef("manager making new log destination")
			m.destinations[name] = NewLogDestination(b)
		}
	}

}
