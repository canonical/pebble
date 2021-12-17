package plan

// Copy returns a deep copy of the plan.
func (p *Plan) Copy() *Plan {
	copied := *p
	copied.Layers = make([]*Layer, len(p.Layers))
	for i, layer := range p.Layers {
		copied.Layers[i] = layer.Copy()
	}
	copied.Services = make(map[string]*Service, len(p.Services))
	for name, service := range p.Services {
		copied.Services[name] = service.Copy()
	}
	copied.Checks = make(map[string]*Check, len(p.Checks))
	for name, check := range p.Checks {
		copied.Checks[name] = check.Copy()
	}
	return &copied
}

// Copy returns a deep copy of the layer.
func (l *Layer) Copy() *Layer {
	copied := *l
	copied.Services = make(map[string]*Service, len(l.Services))
	for name, service := range l.Services {
		copied.Services[name] = service.Copy()
	}
	copied.Checks = make(map[string]*Check, len(l.Checks))
	for name, check := range l.Checks {
		copied.Checks[name] = check.Copy()
	}
	return &copied
}

// Copy returns a deep copy of the service.
func (s *Service) Copy() *Service {
	copied := *s
	copied.After = append([]string(nil), s.After...)
	copied.Before = append([]string(nil), s.Before...)
	copied.Requires = append([]string(nil), s.Requires...)
	if s.Environment != nil {
		copied.Environment = make(map[string]string)
		for k, v := range s.Environment {
			copied.Environment[k] = v
		}
	}
	if s.UserID != nil {
		userID := *s.UserID
		copied.UserID = &userID
	}
	if s.GroupID != nil {
		groupID := *s.GroupID
		copied.GroupID = &groupID
	}
	if s.OnCheckFailure != nil {
		copied.OnCheckFailure = make(map[string]ServiceAction)
		for k, v := range s.OnCheckFailure {
			copied.OnCheckFailure[k] = v
		}
	}
	return &copied
}

// Copy returns a deep copy of the check configuration.
func (c *Check) Copy() *Check {
	copied := *c
	if c.HTTP != nil {
		copied.HTTP = c.HTTP.Copy()
	}
	if c.TCP != nil {
		copied.TCP = c.TCP.Copy()
	}
	if copied.Exec != nil {
		copied.Exec = c.Exec.Copy()
	}
	return &copied
}

// Copy returns a deep copy of the HTTP check configuration.
func (c *HTTPCheck) Copy() *HTTPCheck {
	copied := *c
	if c.Headers != nil {
		copied.Headers = make(map[string]string, len(c.Headers))
		for k, v := range c.Headers {
			copied.Headers[k] = v
		}
	}
	return &copied
}

// Copy returns a deep copy of the TCP check configuration.
func (c *TCPCheck) Copy() *TCPCheck {
	copied := *c
	return &copied
}

// Copy returns a deep copy of the exec check configuration.
func (c *ExecCheck) Copy() *ExecCheck {
	copied := *c
	if c.Environment != nil {
		copied.Environment = make(map[string]string, len(c.Environment))
		for k, v := range c.Environment {
			copied.Environment[k] = v
		}
	}
	if c.UserID != nil {
		userID := *c.UserID
		copied.UserID = &userID
	}
	if c.GroupID != nil {
		groupID := *c.GroupID
		copied.GroupID = &groupID
	}
	return &copied
}
