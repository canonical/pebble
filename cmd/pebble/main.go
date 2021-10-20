// Copyright (c) 2014-2020 Canonical Ltd
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

package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jessevdk/go-flags"

	"golang.org/x/crypto/ssh/terminal"

	"github.com/canonical/pebble/client"
	"github.com/canonical/pebble/internal/logger"
)

var (
	// Standard streams, redirected for testing.
	Stdin  io.Reader = os.Stdin
	Stdout io.Writer = os.Stdout
	Stderr io.Writer = os.Stderr
	// overridden for testing
	ReadPassword = terminal.ReadPassword
	// set to logger.Panicf in testing
	noticef = logger.Noticef
)

// defaultPebbleDir is the Pebble directory used if $PEBBLE is not set. It is
// created by the daemon ("pebble run") if it doesn't exist, and also used by
// the pebble client.
const defaultPebbleDir = "/var/lib/pebble/default"

type options struct {
	Version func() `long:"version"`
}

type argDesc struct {
	name string
	desc string
}

var optionsData options

// ErrExtraArgs is returned  if extra arguments to a command are found
var ErrExtraArgs = fmt.Errorf("too many arguments for command")

// cmdInfo holds information needed to call parser.AddCommand(...).
type cmdInfo struct {
	name, shortHelp, longHelp string
	builder                   func() flags.Commander
	hidden                    bool
	optDescs                  map[string]string
	argDescs                  []argDesc
	alias                     string
	extra                     func(*flags.Command)
}

// commands holds information about all non-debug commands.
var commands []*cmdInfo

// debugCommands holds information about all debug commands.
var debugCommands []*cmdInfo

// addCommand replaces parser.addCommand() in a way that is compatible with
// re-constructing a pristine parser.
func addCommand(name, shortHelp, longHelp string, builder func() flags.Commander, optDescs map[string]string, argDescs []argDesc) *cmdInfo {
	info := &cmdInfo{
		name:      name,
		shortHelp: shortHelp,
		longHelp:  longHelp,
		builder:   builder,
		optDescs:  optDescs,
		argDescs:  argDescs,
	}
	commands = append(commands, info)
	return info
}

// addDebugCommand replaces parser.addCommand() in a way that is
// compatible with re-constructing a pristine parser. It is meant for
// adding debug commands.
func addDebugCommand(name, shortHelp, longHelp string, builder func() flags.Commander, optDescs map[string]string, argDescs []argDesc) *cmdInfo {
	info := &cmdInfo{
		name:      name,
		shortHelp: shortHelp,
		longHelp:  longHelp,
		builder:   builder,
		optDescs:  optDescs,
		argDescs:  argDescs,
	}
	debugCommands = append(debugCommands, info)
	return info
}

type parserSetter interface {
	setParser(*flags.Parser)
}

func lintDesc(cmdName, optName, desc, origDesc string) {
	if len(optName) == 0 {
		logger.Panicf("option on %q has no name", cmdName)
	}
	if len(origDesc) != 0 {
		logger.Panicf("description of %s's %q of %q set from tag", cmdName, optName, origDesc)
	}
	if len(desc) > 0 {
		// decode the first rune instead of converting all of desc into []rune
		r, _ := utf8.DecodeRuneInString(desc)
		// note IsLower != !IsUpper for runes with no upper/lower.
		if unicode.IsLower(r) && !strings.HasPrefix(desc, "login.ubuntu.com") && !strings.HasPrefix(desc, cmdName) {
			noticef("description of %s's %q is lowercase: %q", cmdName, optName, desc)
		}
	}
}

func lintArg(cmdName, optName, desc, origDesc string) {
	lintDesc(cmdName, optName, desc, origDesc)
	if len(optName) > 0 && optName[0] == '<' && optName[len(optName)-1] == '>' {
		return
	}
	if len(optName) > 0 && optName[0] == '<' && strings.HasSuffix(optName, ">s") {
		// see comment in fixupArg about the >s case
		return
	}
	noticef("argument %q's %q should begin with < and end with >", cmdName, optName)
}

func fixupArg(optName string) string {
	// Due to misunderstanding some localized versions of option name are
	// literally "<option>s" instead of "<option>". While translators can
	// improve this over time we can be smarter and avoid silly messages
	// logged whenever the command is used.
	//
	// See: https://bugs.launchpad.net/snapd/+bug/1806761
	if strings.HasSuffix(optName, ">s") {
		return optName[:len(optName)-1]
	}
	return optName
}

type clientSetter interface {
	setClient(*client.Client)
}

type clientMixin struct {
	client *client.Client
}

func (ch *clientMixin) setClient(cli *client.Client) {
	ch.client = cli
}

// Parser creates and populates a fresh parser.
// Since commands have local state a fresh parser is required to isolate tests
// from each other.
func Parser(cli *client.Client) *flags.Parser {
	optionsData.Version = func() {
		printVersions(cli)
		panic(&exitStatus{0})
	}
	flagopts := flags.Options(flags.PassDoubleDash)
	parser := flags.NewParser(&optionsData, flagopts)
	parser.ShortDescription = "Tool to interact with pebble"
	parser.LongDescription = longPebbleDescription
	// hide the unhelpful "[OPTIONS]" from help output
	parser.Usage = ""
	if version := parser.FindOptionByLongName("version"); version != nil {
		version.Description = "Print the version and exit"
		version.Hidden = true
	}
	// add --help like what go-flags would do for us, but hidden
	addHelp(parser)

	// Add all regular commands
	for _, c := range commands {
		obj := c.builder()
		if x, ok := obj.(clientSetter); ok {
			x.setClient(cli)
		}
		if x, ok := obj.(parserSetter); ok {
			x.setParser(parser)
		}

		cmd, err := parser.AddCommand(c.name, c.shortHelp, strings.TrimSpace(c.longHelp), obj)
		if err != nil {
			logger.Panicf("cannot add command %q: %v", c.name, err)
		}
		cmd.Hidden = c.hidden
		if c.alias != "" {
			cmd.Aliases = append(cmd.Aliases, c.alias)
		}

		opts := cmd.Options()
		if c.optDescs != nil && len(opts) != len(c.optDescs) {
			logger.Panicf("wrong number of option descriptions for %s: expected %d, got %d", c.name, len(opts), len(c.optDescs))
		}
		for _, opt := range opts {
			name := opt.LongName
			if name == "" {
				name = string(opt.ShortName)
			}
			desc, ok := c.optDescs[name]
			if !(c.optDescs == nil || ok) {
				logger.Panicf("%s missing description for %s", c.name, name)
			}
			lintDesc(c.name, name, desc, opt.Description)
			if desc != "" {
				opt.Description = desc
			}
		}

		args := cmd.Args()
		if c.argDescs != nil && len(args) != len(c.argDescs) {
			logger.Panicf("wrong number of argument descriptions for %s: expected %d, got %d", c.name, len(args), len(c.argDescs))
		}
		for i, arg := range args {
			name, desc := arg.Name, ""
			if c.argDescs != nil {
				name = c.argDescs[i].name
				desc = c.argDescs[i].desc
			}
			lintArg(c.name, name, desc, arg.Description)
			name = fixupArg(name)
			arg.Name = name
			arg.Description = desc
		}
		if c.extra != nil {
			c.extra(cmd)
		}
	}
	// Add the debug command
	debugCommand, err := parser.AddCommand("debug", shortDebugHelp, longDebugHelp, &cmdDebug{})
	debugCommand.Hidden = true
	if err != nil {
		logger.Panicf("cannot add command %q: %v", "debug", err)
	}
	// Add all the sub-commands of the debug command
	for _, c := range debugCommands {
		obj := c.builder()
		if x, ok := obj.(clientSetter); ok {
			x.setClient(cli)
		}
		cmd, err := debugCommand.AddCommand(c.name, c.shortHelp, strings.TrimSpace(c.longHelp), obj)
		if err != nil {
			logger.Panicf("cannot add debug command %q: %v", c.name, err)
		}
		cmd.Hidden = c.hidden
		opts := cmd.Options()
		if c.optDescs != nil && len(opts) != len(c.optDescs) {
			logger.Panicf("wrong number of option descriptions for %s: expected %d, got %d", c.name, len(opts), len(c.optDescs))
		}
		for _, opt := range opts {
			name := opt.LongName
			if name == "" {
				name = string(opt.ShortName)
			}
			desc, ok := c.optDescs[name]
			if !(c.optDescs == nil || ok) {
				logger.Panicf("%s missing description for %s", c.name, name)
			}
			lintDesc(c.name, name, desc, opt.Description)
			if desc != "" {
				opt.Description = desc
			}
		}

		args := cmd.Args()
		if c.argDescs != nil && len(args) != len(c.argDescs) {
			logger.Panicf("wrong number of argument descriptions for %s: expected %d, got %d", c.name, len(args), len(c.argDescs))
		}
		for i, arg := range args {
			name, desc := arg.Name, ""
			if c.argDescs != nil {
				name = c.argDescs[i].name
				desc = c.argDescs[i].desc
			}
			lintArg(c.name, name, desc, arg.Description)
			name = fixupArg(name)
			arg.Name = name
			arg.Description = desc
		}
	}
	return parser
}

var (
	isStdinTTY  = terminal.IsTerminal(0)
	isStdoutTTY = terminal.IsTerminal(1)
)

// ClientConfig is the configuration of the Client used by all commands.
var clientConfig client.Config

func main() {
	defer func() {
		if v := recover(); v != nil {
			if e, ok := v.(*exitStatus); ok {
				os.Exit(e.code)
			}
			panic(v)
		}
	}()

	if err := run(); err != nil {
		fmt.Fprintf(Stderr, errorPrefix+"%v\n", err)
		os.Exit(1)
	}
}

// exitStatus can be used in panic(&exitStatus{code}) to cause Pebble's main
// function to exit with a given exit code, for the rare cases when you want
// to return an exit code other than 0 or 1, or when an error return is not
// possible.
type exitStatus struct {
	code int
}

func (e *exitStatus) Error() string {
	return fmt.Sprintf("internal error: exitStatus{%d} being handled as normal error", e.code)
}

func run() error {
	logger.SetLogger(logger.New(os.Stderr, "[pebble] "))

	_, clientConfig.Socket = getEnvPaths()

	cli, err := client.New(&clientConfig)
	if err != nil {
		return fmt.Errorf("cannot create client: %v", err)
	}

	parser := Parser(cli)
	xtra, err := parser.Parse()
	if err != nil {
		if e, ok := err.(*flags.Error); ok {
			switch e.Type {
			case flags.ErrCommandRequired:
				printShortHelp()
				return nil
			case flags.ErrHelp:
				parser.WriteHelp(Stdout)
				return nil
			case flags.ErrUnknownCommand:
				sub := os.Args[1]
				sug := "pebble help"
				if len(xtra) > 0 {
					sub = xtra[0]
					if x := parser.Command.Active; x != nil && x.Name != "help" {
						sug = "pebble help " + x.Name
					}
				}
				return fmt.Errorf("unknown command %q, see '%s'.", sub, sug)
			}
		}

		msg, err := errorToMessage(err)
		if err != nil {
			return err
		}

		fmt.Fprintln(Stderr, msg)
		return nil
	}

	maybePresentWarnings(cli.WarningsSummary())

	return nil
}

var errorPrefix = "error: "

func errorToMessage(e error) (normalMessage string, err error) {
	cerr, ok := e.(*client.Error)
	if !ok {
		return "", e
	}

	logger.Debugf("error: %s", cerr)

	isError := true

	var msg string
	switch cerr.Kind {
	case client.ErrorKindLoginRequired:
		u, _ := user.Current()
		if u != nil && u.Username == "root" {
			msg = cerr.Message
		} else {
			msg = fmt.Sprintf(`%s (try with sudo)`, cerr.Message)
		}
	case client.ErrorKindSystemRestart:
		isError = false
		msg = "pebble is about to reboot the system"
	case client.ErrorKindNoDefaultServices:
		msg = "no default services"
	default:
		msg = cerr.Message
	}

	msg = fill(msg, len(errorPrefix))
	if isError {
		return "", errors.New(msg)
	}

	return msg, nil
}

func getEnvPaths() (pebbleDir string, socketPath string) {
	pebbleDir = os.Getenv("PEBBLE")
	if pebbleDir == "" {
		pebbleDir = defaultPebbleDir
	}
	socketPath = os.Getenv("PEBBLE_SOCKET")
	if socketPath == "" {
		socketPath = filepath.Join(pebbleDir, ".pebble.socket")
	}
	return pebbleDir, socketPath
}
