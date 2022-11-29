package logstate

import (
	"fmt"
	"sync"

	"github.com/canonical/pebble/internal/logger"
	"github.com/canonical/pebble/plan"
)

var logBackends = map[string]func(*plan.LogTarget) (LogBackend, error){}

func RegisterLogBackend(name string, builder func(*plan.LogTarget) (LogBackend, error)) {
	validator := func(t *plan.LogTarget) error {
		backend, err := builder(t)
		if backend != nil {
			backend.Close()
		}
		return err
	}
	plan.RegisterLogBackend(name, validator)
}

type LogManager struct {
	mutex   sync.Mutex
	targets map[string]*LogTarget
	// targetsByService maps service name to the list of targets to receive its logs.
	targetsByService map[string][]*LogTarget
	// targetSelection maps log target names to selection criteria.
	targetSelection map[string]string
}

func NewLogManager() *LogManager {
	return &LogManager{
		targets:          make(map[string]*LogTarget),
		targetsByService: make(map[string][]*LogTarget),
		targetSelection:  make(map[string]string),
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
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if len(p.LogTargets) == 0 {
		return
	}

	// update targets
	for name, target := range p.LogTargets {
		backend, err := logBackends[name](target)
		if err != nil {
			logger.Noticef("%v", err)
		}

		orig, ok := m.targets[name]
		if ok {
			// TODO: unhandled error
			orig.SetBackend(backend)
		} else {
			m.targets[name] = NewLogTarget(name, backend)
		}
	}

	// update each service's targets
	for name, service := range p.Services {
		serviceTargets := make([]*LogTarget, len(service.LogTargets))
		for i, targetName := range service.LogTargets {
			serviceTargets[i] = m.targets[targetName]
		}
		m.targetsByService[name] = serviceTargets
	}
}
