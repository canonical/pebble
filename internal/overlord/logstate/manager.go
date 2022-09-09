package logstate

import (
	"fmt"
	"sync"

	"github.com/canonical/pebble/internal/logger"
	"github.com/canonical/pebble/internal/plan"
)

type LogManager struct {
	mutex                 sync.Mutex
	destinations          map[string]*LogDestination
	destinationsByService map[string][]*LogDestination
}

func NewLogManager() *LogManager {
	return &LogManager{
		destinations:          make(map[string]*LogDestination),
		destinationsByService: make(map[string][]*LogDestination),
	}
}

func (m *LogManager) GetDestinations(serviceName string) ([]*LogDestination, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	dests, ok := m.destinationsByService[serviceName]
	if !ok {
		return nil, fmt.Errorf("no known logging destinations for service %q", serviceName)
	}
	return dests, nil
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
			m.destinations[name] = NewLogDestination(name, b)
		}
	}

	// update each service's destinations
	// TODO: update this with the appropriate config we settle on for defaults, explicit, all, etc.
	for name, service := range p.Services {
		m.destinationsByService[name] = make([]*LogDestination, 0)
		if len(service.LogDestinations) == 0 {
			// by default forward service's logs to all destinations
			for _, dest := range m.destinations {
				m.destinationsByService[name] = append(m.destinationsByService[name], dest)
			}
		} else {
			// only forward to explicitly named destinations
			for _, destName := range service.LogDestinations {
				m.destinationsByService[name] = append(m.destinationsByService[name], m.destinations[destName])
			}
		}
	}
}
