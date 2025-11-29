package plan

import (
	"fmt"
	"strconv"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"
)

type OptionalDuration struct {
	Value time.Duration
	IsSet bool
}

func (o OptionalDuration) IsZero() bool {
	return !o.IsSet
}

func (o OptionalDuration) MarshalYAML() (any, error) {
	if !o.IsSet {
		return nil, nil
	}
	return o.Value.String(), nil
}

func (o *OptionalDuration) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.ScalarNode {
		return fmt.Errorf("duration must be a YAML string")
	}
	duration, err := time.ParseDuration(value.Value)
	if err != nil {
		return fmt.Errorf("invalid duration %q", value.Value)
	}
	o.Value = duration
	o.IsSet = true
	return nil
}

type OptionalFloat struct {
	Value float64
	IsSet bool
}

func (o OptionalFloat) IsZero() bool {
	return !o.IsSet
}

func (o OptionalFloat) MarshalYAML() (any, error) {
	if !o.IsSet {
		return nil, nil
	}
	return o.Value, nil
}

func (o *OptionalFloat) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.ScalarNode {
		return fmt.Errorf("value must be a YAML number")
	}
	n, err := strconv.ParseFloat(value.Value, 64)
	if err != nil {
		return fmt.Errorf("invalid floating-point number %q", value.Value)
	}
	o.Value = n
	o.IsSet = true
	return nil
}

// OptionalSyscallSignal is a wrapper around syscall.Signal
type OptionalSyscallSignal struct {
	Value syscall.Signal
	Name  string
	IsSet bool
}

func (o OptionalSyscallSignal) IsZero() bool {
	return !o.IsSet
}

func (o OptionalSyscallSignal) MarshalYAML() (any, error) {
	if !o.IsSet {
		return nil, nil
	}
	return o.Name, nil
}

func (o *OptionalSyscallSignal) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.ScalarNode {
		return fmt.Errorf("signal must be a YAML string")
	}
	signal, err := parseSignal(value.Value)
	if err != nil {
		return fmt.Errorf("invalid signal %q: %w", value.Value, err)
	}
	o.Value = signal
	o.Name = value.Value
	o.IsSet = true
	return nil
}

// A selection of the most common signals.
var signals = map[string]syscall.Signal{
	"SIGABRT":   syscall.SIGABRT,
	"SIGALRM":   syscall.SIGALRM,
	"SIGBUS":    syscall.SIGBUS,
	"SIGCHLD":   syscall.SIGCHLD,
	"SIGCLD":    syscall.SIGCLD,
	"SIGCONT":   syscall.SIGCONT,
	"SIGFPE":    syscall.SIGFPE,
	"SIGHUP":    syscall.SIGHUP,
	"SIGILL":    syscall.SIGILL,
	"SIGINT":    syscall.SIGINT,
	"SIGIO":     syscall.SIGIO,
	"SIGIOT":    syscall.SIGIOT,
	"SIGKILL":   syscall.SIGKILL,
	"SIGPIPE":   syscall.SIGPIPE,
	"SIGPOLL":   syscall.SIGPOLL,
	"SIGPROF":   syscall.SIGPROF,
	"SIGPWR":    syscall.SIGPWR,
	"SIGQUIT":   syscall.SIGQUIT,
	"SIGSEGV":   syscall.SIGSEGV,
	"SIGSTKFLT": syscall.SIGSTKFLT,
	"SIGSTOP":   syscall.SIGSTOP,
	"SIGSYS":    syscall.SIGSYS,
	"SIGTERM":   syscall.SIGTERM,
	"SIGTRAP":   syscall.SIGTRAP,
	"SIGTSTP":   syscall.SIGTSTP,
	"SIGTTIN":   syscall.SIGTTIN,
	"SIGTTOU":   syscall.SIGTTOU,
	"SIGUNUSED": syscall.SIGUNUSED,
	"SIGURG":    syscall.SIGURG,
	"SIGUSR1":   syscall.SIGUSR1,
	"SIGUSR2":   syscall.SIGUSR2,
	"SIGVTALRM": syscall.SIGVTALRM,
	"SIGWINCH":  syscall.SIGWINCH,
	"SIGXCPU":   syscall.SIGXCPU,
	"SIGXFSZ":   syscall.SIGXFSZ,
}

func parseSignal(s string) (syscall.Signal, error) {
	if sig, ok := signals[s]; ok {
		return sig, nil
	}
	return 0, fmt.Errorf("unknown signal %q", s)
}

func SignalToString(sig syscall.Signal) string {
	for name, s := range signals {
		if s == sig {
			return name
		}
	}
	return sig.String()
}
