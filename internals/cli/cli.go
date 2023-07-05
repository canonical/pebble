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

package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/canonical/go-flags"

	"golang.org/x/crypto/ssh/terminal"

	"github.com/canonical/pebble/client"
	"github.com/canonical/pebble/internals/logger"
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

// ErrExtraArgs is returned  if extra arguments to a command are found
var ErrExtraArgs = fmt.Errorf("too many arguments for command")

// CmdInfo holds information needed by the CLI to execute commands and
// populate entries in the help manual.
type CmdInfo struct {
	// Name of the command
	Name string

	// Summary is a single-line help string that will be displayed
	// in the full Pebble help manual (i.e. help --all)
	Summary string

	// Description contains exhaustive documentation about the command,
	// that will be reflected in the specific help manual for the
	// command, and in the Pebble man page.
	Description string

	// Builder is a function that creates a new instance of the command
	// struct containing an Execute(args []string) implementation.
	Builder func() flags.Commander

	// ArgsHelp (optional) contains help about the command-line arguments
	// (including options) supported by the command.
	//
	//  map[string]string{
	//      "--long-option": "my very long option",
	//      "-v": "verbose output",
	//      "<change-id>": "named positional argument"
	//  }
	ArgsHelp map[string]string

	// Whether to pass all arguments after the first non-option as remaining
	// command line arguments. This is equivalent to strict POSIX processing.
	PassAfterNonOption bool

	// When set, the command will be a subcommand of the `debug` command.
	// Do not set this flag manually, use AddDebugCommand instead.
	debug bool
}

// commands holds information about all the regular Pebble commands.
var commands []*CmdInfo

// debugCommands holds information about all the subcommands of the `pebble debug` command.
var debugCommands []*CmdInfo

// AddCommand adds a command to the top-level parser.
func AddCommand(info *CmdInfo) {
	commands = append(commands, info)
}

// AddDebugCommand adds a subcommand to the `debug` command.
func AddDebugCommand(info *CmdInfo) {
	info.debug = true
	debugCommands = append(debugCommands, info)
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

type parserSetter interface {
	setParser(*flags.Parser)
}

type defaultOptions struct {
	Version func() `long:"version" hidden:"yes" description:"Print the version and exit"`
}

// Parser creates and populates a fresh parser.
// Since commands have local state a fresh parser is required to isolate tests
// from each other.
func Parser(cli *client.Client) *flags.Parser {
	// Implement --version by default on every command
	defaultOpts := defaultOptions{
		Version: func() {
			printVersions(cli)
			panic(&exitStatus{0})
		},
	}

	flagOpts := flags.Options(flags.PassDoubleDash)
	parser := flags.NewParser(&defaultOpts, flagOpts)
	parser.ShortDescription = "Tool to interact with pebble"
	parser.LongDescription = longPebbleDescription

	// Add --help like what go-flags would do for us, but hidden
	addHelp(parser)

	// Regular expressions for positional and flag arguments
	positionalRegexp := regexp.MustCompile(`^<[\w-]+>$`)
	flagRegexp := regexp.MustCompile(`^-(\w|-[\w]+(-?[\w]+)*)$`)

	// Create debug command
	debugCmd, err := parser.AddCommand("debug", cmdDebugSummary, cmdDebugDescription, &cmdDebug{})
	debugCmd.Hidden = true
	if err != nil {
		logger.Panicf("cannot add command %q: %v", "debug", err)
	}

	// Add all commands
	for _, c := range append(commands, debugCommands...) {
		obj := c.Builder()
		if x, ok := obj.(clientSetter); ok {
			x.setClient(cli)
		}
		if x, ok := obj.(parserSetter); ok {
			x.setParser(parser)
		}

		// Target either the `debug` command or the top-level parser depending on this command
		// being or not marked as debug.
		var target *flags.Command
		if c.debug {
			target = debugCmd
		} else {
			target = parser.Command
		}

		cmd, err := target.AddCommand(c.Name, c.Summary, strings.TrimSpace(c.Description), obj)
		if err != nil {
			logger.Panicf("cannot add command %q: %v", c.Name, err)
		}
		cmd.PassAfterNonOption = c.PassAfterNonOption

		// Extract help for flags and positional arguments from ArgsHelp
		flagHelp := map[string]string{}
		positionalHelp := map[string]string{}

		for specifier, help := range c.ArgsHelp {
			if flagRegexp.MatchString(specifier) {
				flagHelp[specifier] = help
			} else if positionalRegexp.MatchString(specifier) {
				positionalHelp[specifier] = help
			} else {
				logger.Panicf("invalid help specifier from %q: %q", c.Name, specifier)
			}
		}

		// Make sure all argument descriptions are set
		opts := cmd.Options()
		if len(opts) != len(flagHelp) {
			logger.Panicf("wrong number of flag descriptions for %q: expected %d, got %d", c.Name, len(opts), len(flagHelp))
		}
		for _, opt := range opts {
			if description, ok := flagHelp["--"+opt.LongName]; ok {
				lintDesc(c.Name, opt.LongName, description, opt.Description)
				opt.Description = description
			} else if description, ok := flagHelp["-"+string(opt.ShortName)]; ok {
				lintDesc(c.Name, string(opt.ShortName), description, opt.Description)
				opt.Description = description
			} else if !opt.Hidden {
				logger.Panicf("%q missing description for %q", c.Name, opt)
			}
		}

		args := cmd.Args()
		for _, arg := range args {
			if description, ok := positionalHelp[arg.Name]; ok {
				lintArg(c.Name, arg.Name, description, arg.Description)
				arg.Name = fixupArg(arg.Name)
				arg.Description = description
			}
		}
	}
	return parser
}

var (
	isStdinTTY  = terminal.IsTerminal(0)
	isStdoutTTY = terminal.IsTerminal(1)
	osExit      = os.Exit
)

// ClientConfig is the configuration of the Client used by all commands.
var clientConfig client.Config

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

func Run() error {
	defer func() {
		if v := recover(); v != nil {
			if e, ok := v.(*exitStatus); ok {
				osExit(e.code)
			}
			panic(v)
		}
	}()

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
				return fmt.Errorf("unknown command %q, see '%s'", sub, sug)
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
