package cli

import (
	"fmt"

	"github.com/canonical/go-flags"

	"github.com/canonical/pebble/internals/logger"
)

const shortEnterHelp = "Run subcommand under a container environment"
const longEnterHelp = `
The enter command facilitates the use of Pebble as an entrypoint for containers.
When used without a subcommand it mimics the behavior of the run command
alone, while if used with a subcommand it runs that subcommand in the most
appropriate environment taking into account its purpose.

These subcommands are currently supported:

  help      (1)(2)
  version   (1)(2)
  plan      (1)(2)
  services  (1)(2)
  ls        (1)(2)
  exec      (2)
  start     (3)
  stop      (3)

(1) Services are not started.
(2) No logs on stdout unless -v is used.
(3) Services continue running after the subcommand succeeds.
`

type enterFlags int

const (
	enterSilenceLogging enterFlags = 1 << iota
	enterNoServiceManager
	enterKeepServiceManager
	enterRequireServiceAutostart
	enterProhibitServiceAutostart
)

type cmdEnter struct {
	clientMixin
	sharedRunEnterOpts
	Run        bool `long:"run"`
	Positional struct {
		Cmd []string `positional-arg-name:"<subcommand>"`
	} `positional-args:"yes"`
	parser *flags.Parser
}

func init() {
	optsHelp := map[string]string{
		"run": "Start default services before executing subcommand",
	}
	for k, v := range sharedRunEnterOptsHelp {
		optsHelp[k] = v
	}
	cmdInfo := AddCommand("enter", shortEnterHelp, longEnterHelp, func() flags.Commander { return &cmdEnter{} }, optsHelp, nil)
	cmdInfo.extra = func(cmd *flags.Command) {
		cmd.PassAfterNonOption = true
	}
}

func commandEnterFlags(commander flags.Commander) (enterFlags enterFlags, supported bool) {
	supported = true
	switch commander.(type) {
	case *cmdExec:
		enterFlags = enterSilenceLogging
	case *cmdHelp:
		enterFlags = enterNoServiceManager
	case *cmdLs, *cmdPlan, *cmdServices, *cmdVersion:
		enterFlags = enterSilenceLogging | enterProhibitServiceAutostart
	case *cmdStart:
		enterFlags = enterKeepServiceManager
	case *cmdStop:
		enterFlags = enterKeepServiceManager | enterRequireServiceAutostart
	default:
		supported = false
	}
	return
}

func (cmd *cmdEnter) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	runCmd := cmdRun{
		sharedRunEnterOpts: cmd.sharedRunEnterOpts,
	}
	runCmd.setClient(cmd.client)

	if len(cmd.Positional.Cmd) == 0 {
		runCmd.run(nil)
		return nil
	}

	runCmd.Hold = !cmd.Run

	var (
		commander flags.Commander
		extraArgs []string
	)

	parser := Parser(cmd.client)
	parser.CommandHandler = func(c flags.Commander, a []string) error {
		commander = c
		extraArgs = a
		return nil
	}

	if _, err := parser.ParseArgs(cmd.Positional.Cmd); err != nil {
		cmd.parser.Command.Active = parser.Command.Active
		return err
	}

	if parser.Active == nil {
		panic("internal error: expected subcommand (parser.Active == nil)")
	}

	enterFlags, supported := commandEnterFlags(commander)

	if !supported {
		return fmt.Errorf("enter: subcommand %q is not supported", parser.Active.Name)
	}

	if enterFlags&enterRequireServiceAutostart != 0 && !cmd.Run {
		return fmt.Errorf("enter: must use --run before %q subcommand", parser.Active.Name)
	}

	if enterFlags&(enterProhibitServiceAutostart|enterNoServiceManager) != 0 && cmd.Run {
		return fmt.Errorf("enter: cannot provide --run before %q subcommand", parser.Active.Name)
	}

	if enterFlags&enterNoServiceManager != 0 {
		if err := commander.Execute(extraArgs); err != nil {
			cmd.parser.Command.Active = parser.Command.Active
			return err
		}
		return nil
	}

	if enterFlags&enterSilenceLogging != 0 && !cmd.Verbose {
		logger.SetLogger(logger.NullLogger)
	}

	runReadyCh := make(chan func(), 1)
	runResultCh := make(chan interface{})
	var runStop func()

	go func() {
		defer func() { runResultCh <- recover() }()
		runCmd.run(runReadyCh)
	}()

	select {
	case runStop = <-runReadyCh:
	case runPanic := <-runResultCh:
		if runPanic == nil {
			panic("internal error: pebble daemon stopped early")
		}
		panic(runPanic)
	}

	err := commander.Execute(extraArgs)

	if err != nil {
		cmd.parser.Command.Active = parser.Command.Active
	}

	if err != nil || enterFlags&enterKeepServiceManager == 0 {
		runStop()
	}

	if runPanic := <-runResultCh; runPanic != nil {
		panic(runPanic)
	}

	return err
}

func (cmd *cmdEnter) setParser(parser *flags.Parser) {
	cmd.parser = parser
}
