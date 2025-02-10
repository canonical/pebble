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
	"bytes"
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/canonical/go-flags"

	cmdpkg "github.com/canonical/pebble/cmd"
)

const cmdHelpSummary = "Show help about a command"
const cmdHelpDescription = `
The help command displays information about commands.
`

type cmdHelp struct {
	parser *flags.Parser

	All        bool `long:"all"`
	Manpage    bool `long:"man" hidden:"true"`
	Positional struct {
		Subs []string `positional-arg-name:"<command>"`
	} `positional-args:"yes"`
}

func init() {
	AddCommand(&CmdInfo{
		Name:        "help",
		Summary:     cmdHelpSummary,
		Description: cmdHelpDescription,
		ArgsHelp: map[string]string{
			"--all": "Show a short summary of all commands",
			"--man": "Generate the manpage",
		},
		New: func(opts *CmdOptions) flags.Commander {
			return &cmdHelp{parser: opts.Parser}
		},
	})
}

// addHelp adds --help like what go-flags would do for us, but hidden
func addHelp(parser *flags.Parser) error {
	var help struct {
		ShowHelp func() error `short:"h" long:"help"`
	}
	help.ShowHelp = func() error {
		// this function is called via --help (or -h). In that
		// case, parser.Command.Active should be the command
		// on which help is being requested (like "pebble foo
		// --help", active is foo), or nil in the toplevel.
		if parser.Command.Active == nil {
			// this means *either* a bare 'pebble --help',
			// *or* 'pebble --help command'
			//
			// If we return nil in the first case go-flags
			// will throw up an ErrCommandRequired on its
			// own, but in the second case it'll go on to
			// run the command, which is very unexpected.
			//
			// So we force the ErrCommandRequired here.

			// toplevel --help gets handled via ErrCommandRequired
			return &flags.Error{Type: flags.ErrCommandRequired}
		}
		// not toplevel, so ask for regular help
		return &flags.Error{Type: flags.ErrHelp}
	}
	hlpgrp, err := parser.AddGroup("Help Options", "", &help)
	if err != nil {
		return err
	}
	hlpgrp.Hidden = true
	hlp := parser.FindOptionByLongName("help")
	hlp.Description = "Show this help message"
	hlp.Hidden = true

	return nil
}

func (cmd *cmdHelp) setParser(parser *flags.Parser) {
	cmd.parser = parser
}

// manfixer is a hackish way to fix drawbacks in the generated manpage:
// - no way to get it into section 8
// - duplicated TP lines that break older groff (e.g. 14.04), lp:1814767
type manfixer struct {
	bytes.Buffer
	done        bool
	programName string
}

func (w *manfixer) Write(buf []byte) (int, error) {
	if !w.done {
		w.done = true
		// Find the .TH <program name> prefix.
		prefix := ".TH " + w.programName + " "
		if bytes.HasPrefix(buf, []byte(prefix)) {
			// io.Writer.Write must not modify the buffer, even temporarily.
			// Write all characters up to the prefix.
			n, _ := w.Buffer.Write(buf[:len(prefix)])
			// Do not write the character after the prefix (originally, '1'),
			// and write an '8' instead, for fixing up the man section.
			w.Buffer.Write([]byte{'8'})
			// Write everything after the original '1' character.
			m, err := w.Buffer.Write(buf[1+len(prefix):])
			return n + m + 1, err
		}
	}
	return w.Buffer.Write(buf)
}

var tpRegexp = regexp.MustCompile(`(?m)(?:^\.TP\n)+`)

func (w *manfixer) flush() {
	str := tpRegexp.ReplaceAllLiteralString(w.Buffer.String(), ".TP\n")
	io.Copy(Stdout, strings.NewReader(str))
}

func (cmd cmdHelp) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}
	if cmd.Manpage {
		// you shouldn't try to to combine --man with --all nor a
		// subcommand, but --man is hidden so no real need to check.
		out := &manfixer{programName: cmd.parser.Name}
		cmd.parser.WriteManPage(out)
		out.flush()
		return nil
	}
	if cmd.All {
		if len(cmd.Positional.Subs) > 0 {
			return fmt.Errorf("help accepts a command, or '--all', but not both.")
		}
		printLongHelp(cmd.parser)
		return nil
	}

	var subcmd = cmd.parser.Command
	for _, subname := range cmd.Positional.Subs {
		subcmd = subcmd.Find(subname)
		if subcmd == nil {
			sug := cmdpkg.ProgramName + " help"
			if x := cmd.parser.Command.Active; x != nil && x.Name != "help" {
				sug = cmdpkg.ProgramName + " help " + x.Name
			}
			return fmt.Errorf("unknown command %q, see '%s'.", subname, sug)
		}
		// this makes "pebble help foo" work the same as "pebble foo --help"
		cmd.parser.Command.Active = subcmd
	}
	if subcmd != cmd.parser.Command {
		return &flags.Error{Type: flags.ErrHelp}
	}
	return &flags.Error{Type: flags.ErrCommandRequired}
}

type HelpCategory struct {
	Label       string
	Description string
	Commands    []string
}

// HelpCategories helps us by grouping commands
var HelpCategories = []HelpCategory{{
	Label:       "Run",
	Description: "run the service manager",
	Commands:    []string{"run"},
}, {
	Label:       "Info",
	Description: "help and version information",
	Commands:    []string{"help", "version"},
}, {
	Label:       "Plan",
	Description: "view and change configuration",
	Commands:    []string{"add", "plan", "replan"},
}, {
	Label:       "Services",
	Description: "manage services",
	Commands:    []string{"services", "logs", "start", "restart", "signal", "stop"},
}, {
	Label:       "Checks",
	Description: "manage health checks",
	Commands:    []string{"checks", "start-checks", "stop-checks", "health"},
}, {
	Label:       "Files",
	Description: "work with files and execute commands",
	Commands:    []string{"push", "pull", "ls", "mkdir", "rm", "exec"},
}, {
	Label:       "Changes",
	Description: "manage changes and their tasks",
	Commands:    []string{"changes", "tasks"},
}, {
	Label:       "Notices",
	Description: "manage notices and warnings",
	Commands:    []string{"warnings", "okay", "notices", "notice", "notify"},
}, {
	Label:       "Identities", // special-cased in printShortHelp
	Description: "manage user identities",
	Commands:    []string{"identities", "identity", "add-identities", "update-identities", "remove-identities"},
}}

var (
	HelpHeader = strings.TrimSpace(`
{{.DisplayName}} lets you control services and perform management actions on
the system that is running them.
`)
	pebbleUsage               = "Usage: {{.ProgramName}} <command> [<options>...]"
	pebbleHelpCategoriesIntro = "Commands can be classified as follows:"

	HelpFooter = strings.TrimSpace(`
Set the PEBBLE environment variable to override the configuration directory
(which defaults to {{.DefaultDir}}). Set PEBBLE_SOCKET to override
the unix socket used for the API (defaults to $PEBBLE/.pebble.socket).
`)

	pebbleHelpAllFooter = "For more information about a command, run '{{.ProgramName}} help <command>'."
	pebbleHelpFooter    = "For a short summary of all commands, run '{{.ProgramName}} help --all'."
)

func printHelpHeader() {
	fmt.Fprintln(Stdout, applyPersonality(HelpHeader))
	fmt.Fprintln(Stdout)
	fmt.Fprintln(Stdout, applyPersonality(pebbleUsage))
	fmt.Fprintln(Stdout)
	fmt.Fprintln(Stdout, applyPersonality(pebbleHelpCategoriesIntro))
}

func printHelpAllFooter() {
	fmt.Fprintln(Stdout)
	fmt.Fprintln(Stdout, applyPersonality(HelpFooter))
	fmt.Fprintln(Stdout)
	fmt.Fprintln(Stdout, applyPersonality(pebbleHelpAllFooter))
}

func printHelpFooter() {
	printHelpAllFooter()
	fmt.Fprintln(Stdout, applyPersonality(pebbleHelpFooter))
}

// this is called when the Execute returns a flags.Error with ErrCommandRequired
func printShortHelp() {
	printHelpHeader()
	fmt.Fprintln(Stdout)
	maxLen := 0
	for _, categ := range HelpCategories {
		if l := utf8.RuneCountInString(categ.Label); l > maxLen {
			maxLen = l
		}
	}
	for _, categ := range HelpCategories {
		commandsStr := strings.Join(categ.Commands, ", ")
		if categ.Label == "Identities" {
			// Special case for identities command to avoid a long list here
			commandsStr = "identities --help"
		}
		fmt.Fprintf(Stdout, "%*s: %s\n", maxLen+2, categ.Label, commandsStr)
	}
	printHelpFooter()
}

// this is "pebble help --all"
func printLongHelp(parser *flags.Parser) {
	printHelpHeader()
	maxLen := 0
	for _, categ := range HelpCategories {
		for _, command := range categ.Commands {
			if l := len(command); l > maxLen {
				maxLen = l
			}
		}
	}

	// flags doesn't have a LookupCommand?
	commands := parser.Commands()
	cmdLookup := make(map[string]*flags.Command, len(commands))
	for _, cmd := range commands {
		cmdLookup[cmd.Name] = cmd
	}

	for _, categ := range HelpCategories {
		fmt.Fprintln(Stdout)
		fmt.Fprintf(Stdout, "  %s (%s):\n", categ.Label, categ.Description)
		for _, name := range categ.Commands {
			cmd := cmdLookup[name]
			if cmd == nil {
				fmt.Fprintf(Stderr, "??? Cannot find command %q mentioned in help categories, please report!\n", name)
			} else {
				fmt.Fprintf(Stdout, "    %*s  %s\n", -maxLen, name, cmd.ShortDescription)
			}
		}
	}
	printHelpAllFooter()
}
