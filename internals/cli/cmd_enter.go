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

package cli

import (
	"fmt"

	"github.com/canonical/go-flags"

	"github.com/canonical/pebble/client"
	"github.com/canonical/pebble/internals/logger"
)

const cmdEnterSummary = "Run subcommand under a container environment"
const cmdEnterDescription = `
The enter command facilitates the use of {{.DisplayName}} as an entrypoint for containers.
When used without a subcommand it mimics the behavior of the run command
alone, while if used with a subcommand it runs that subcommand in the most
appropriate environment taking into account its purpose.

These subcommands are currently supported:

  help      (1)(2)
  version   (1)(2)
  plan      (1)(2)
  services  (1)(2)
  ls        (1)(2)
  start     (3)
  stop      (3)

(1) Services are not started.
(2) No logs on stdout unless -v/--verbose is used.
(3) Services continue running after the subcommand succeeds.
`

type cmdEnter struct {
	client *client.Client
	parser *flags.Parser

	pebbleDir  string
	socketPath string

	sharedRunEnterOpts
	Run        bool `long:"run"`
	Positional struct {
		Cmd []string `positional-arg-name:"<subcommand>"`
	} `positional-args:"yes"`
}

func init() {
	AddCommand(&CmdInfo{
		Name:        "enter",
		Summary:     cmdEnterSummary,
		Description: cmdEnterDescription,
		New: func(opts *CmdOptions) flags.Commander {
			return &cmdEnter{
				client:     opts.Client,
				parser:     opts.Parser,
				pebbleDir:  opts.PebbleDir,
				socketPath: opts.SocketPath,
			}
		},
		ArgsHelp: merge(sharedRunEnterArgsHelp, map[string]string{
			"--run": "Start default services before executing subcommand",
		}),
		PassAfterNonOption: true,
	})
}

type enterFlags int

const (
	// If set, disable all logs unless --verbose is passed.
	enterSilenceLogging enterFlags = 1 << iota
	// If set, do not allow the usage of --verbose option with subcommand.
	enterProhibitVerbose
	// If set, do not run the pebble daemon.
	enterNoServiceManager
	// If set, keep the pebble daemon running, even after the subcommand
	// execution has finished.
	enterKeepServiceManager
	// If set, default services (with startup: enabled) must be started before
	// executing the subcommand.
	enterRequireServiceAutostart
	// If set, do not start the default services (with startup: enabled).
	// Behaviour similar to "--hold".
	enterProhibitServiceAutostart
)

func commandEnterFlags(commander flags.Commander) (enterFlags enterFlags, supported bool) {
	supported = true
	switch commander.(type) {
	case *cmdExec:
		enterFlags = enterSilenceLogging | enterProhibitVerbose
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
		client:             cmd.client,
		pebbleDir:          cmd.pebbleDir,
		socketPath:         cmd.socketPath,
	}

	if len(cmd.Positional.Cmd) == 0 {
		runCmd.run(nil)
		return nil
	}

	runCmd.Hold = !cmd.Run

	var (
		commander flags.Commander
		extraArgs []string
	)

	parser := Parser(&ParserOptions{
		Client:     cmd.client,
		PebbleDir:  cmd.pebbleDir,
		SocketPath: cmd.socketPath,
	})
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

	if enterFlags&enterProhibitVerbose != 0 && cmd.Verbose {
		return fmt.Errorf("enter: cannot provide -v/--verbose before %q subcommand", parser.Active.Name)
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
	runResultCh := make(chan any)
	var runStop func()

	go func() {
		defer func() { runResultCh <- recover() }()
		runCmd.run(runReadyCh)
	}()

	select {
	case runStop = <-runReadyCh:
	case runPanic := <-runResultCh:
		if runPanic == nil {
			panic("internal error: daemon stopped early")
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
