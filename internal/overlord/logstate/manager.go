package logstate

import (
	"fmt"
	"sync"

	"github.com/canonical/pebble/internal/logger"
	"github.com/canonical/pebble/internal/plan"
)

const (
	logSelectionOptOut  = "opt-out"
	logSelectionOptIn   = "opt-in"
	logSelectionDisable = "disable"
)

type LogManager struct {
	mutex   sync.Mutex
	targets map[string]*LogTarget
	// targetSelection maps log target names to selection criteria.
	targetSelection map[string]string
	// targetsByService maps service name to the list of targets to receive its logs.
	targetsByService map[string][]*LogTarget
}

func NewLogManager() *LogManager {
	return &LogManager{
		targets:          make(map[string]*LogTarget),
		targetsByService: make(map[string][]*LogTarget),
	}
}

func (m *LogManager) GetTargets(serviceName string) ([]*LogTarget, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	targets, ok := m.targetsByService[serviceName]
	if !ok {
		return nil, fmt.Errorf("no known logging targets for service %q", serviceName)
	}
	return targets, nil
}

// PlanChanged handles updates to the plan (server configuration),
// stopping the previous checks and starting the new ones as required.
func (m *LogManager) PlanChanged(p *plan.Plan) {
	if len(p.LogTargets) == 0 {
		return
	}

	m.mutex.Lock()
	defer m.mutex.Unlock()

	// update targets
	for name, target := range p.LogTargets {
		var b LogBackend
		var err error
		switch target.Type {
		case "loki":
			b, err = NewLokiBackend(target.Location)
		case "syslog":
			b, err = NewSyslogBackend(target.Location)
		default:
			logger.Noticef("unsupported logging target type: %v", target.Type)
			continue
		}
		if err != nil {
			logger.Noticef("invalid config for log target %q: %v", name, err)
			continue
		}
		switch target.Selection {
		case "":
			m.targetSelection[name] = logSelectionOptOut
		case logSelectionOptIn, logSelectionOptOut, logSelectionDisable:
			m.targetSelection[name] = target.Selection
		default:
			logger.Noticef("invalid selection for log target %q: %v", name, target.Selection)
			continue
		}

		orig, ok := m.targets[name]
		if ok {
			orig.SetBackend(b)
		} else {
			m.targets[name] = NewLogTarget(name, b)
		}

	}

	// update each service's targets
	// TODO: update this with the appropriate config we settle on for defaults, explicit, all, etc.
	for name, service := range p.Services {
		m.targetsByService[name] = make([]*LogTarget, 0)
		if len(service.LogTargets) == 0 {
			for targetName, target := range m.targets {
				switch m.targetSelection[targetName] {
				case logSelectionOptIn, logSelectionDisable: // skip
				case logSelectionOptOut:
					m.targetsByService[name] = append(m.targetsByService[name], target)
				}
			}
		} else {
			for _, targetName := range service.LogTargets {
				switch m.targetSelection[targetName] {
				case logSelectionDisable: // skip
				case logSelectionOptOut, logSelectionOptIn:
					m.targetsByService[name] = append(m.targetsByService[name], m.targets[targetName])
				}
			}
		}
	}
}
