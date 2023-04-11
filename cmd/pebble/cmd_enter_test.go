// SPDX-FileCopyrightText: 2023 Canonical Ltd
// SPDX-License-Identifier: GPL-3.0-only

package main_test

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	. "gopkg.in/check.v1"

	pebble "github.com/canonical/pebble/cmd/pebble"
)

func dumbDedent(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\t", "")
	s += "\n"
	return s
}

func writeTemplate(filename string, templateString string, templateData interface{}) {
	os.MkdirAll(filepath.Dir(filename), 0755)

	t, err := template.New(filename).Parse(templateString)
	if err != nil {
		panic(err)
	}

	f, err := os.Create(filename)
	if err != nil {
		panic(err)
	}
	defer func() { f.Close() }()

	err = t.Execute(f, templateData)
	if err != nil {
		panic(err)
	}
}

func writeMessageServices(s *PebbleSuite) {
	serviceTemplate := dumbDedent(`
		#!/bin/sh
		echo "writing message $1"
		echo $1 >$PEBBLE/$2
		sleep 999
	`)
	servicePath := filepath.Join(s.pebbleDir, "write-message")

	writeTemplate(servicePath, serviceTemplate, nil)
	os.Chmod(servicePath, 0755)

	layerTemplate := dumbDedent(`
		summary: message services
		services:
		  write-message-01:
		    override: replace
		    command: {{.pebbleDir}}/write-message foo msg1
		    startup: enabled
		  write-message-02:
		    override: merge
		    command: {{.pebbleDir}}/write-message bar msg2
		    startup: disabled
	`)
	layerTemplateData := map[string]string{"pebbleDir": s.pebbleDir}
	layerPath := filepath.Join(s.pebbleDir, "layers", "001-messages.yaml")

	writeTemplate(layerPath, layerTemplate, layerTemplateData)
}

func (s *PebbleSuite) TestEnterHelpCommand(c *C) {
	restore := fakeArgs("pebble", "enter", "help")
	defer restore()

	exitCode := pebble.PebbleMain()
	c.Check(s.Stderr(), Equals, "")
	c.Check(s.Stdout(), Matches, "(?s)Pebble lets you control services.*Commands can be classified as follows.*")
	c.Check(s.Stdout(), Not(Matches), "^(?s)Usage:\n  pebble enter \\[enter-OPTIONS\\] \\[<subcommand>\\.\\.\\.\\].*")
	c.Check(exitCode, Equals, 0)
}

func (s *PebbleSuite) TestEnterHelpOption(c *C) {
	restore := fakeArgs("pebble", "enter", "--help")
	defer restore()

	exitCode := pebble.PebbleMain()
	c.Check(s.Stderr(), Equals, "")
	c.Check(s.Stdout(), Not(Matches), "(?s)Pebble lets you control services.*Commands can be classified as follows.*")
	c.Check(s.Stdout(), Matches, "^(?s)Usage:\n  pebble enter \\[enter-OPTIONS\\] \\[<subcommand>\\.\\.\\.\\].*")
	c.Check(exitCode, Equals, 0)
}

func (s *PebbleSuite) TestEnterUnknownCommand(c *C) {
	restore := fakeArgs("pebble", "enter", "foo")
	defer restore()

	exitCode := pebble.PebbleMain()
	c.Check(s.Stderr(), Equals, "error: unknown command \"foo\", see 'pebble help'.\n")
	c.Check(s.Stdout(), Equals, "")
	c.Check(exitCode, Equals, 1)
}

func (s *PebbleSuite) TestEnterServicesStatus(c *C) {
	expectedOutput := dumbDedent(`
		Service           Startup   Current   Since
		write-message-01  enabled   inactive  -
		write-message-02  disabled  inactive  -
	`)

	writeMessageServices(s)

	restore := fakeArgs("pebble", "enter", "services")
	defer restore()

	exitCode := pebble.PebbleMain()
	c.Check(s.Stderr(), Equals, "")
	c.Check(s.Stdout(), Equals, expectedOutput)
	c.Check(exitCode, Equals, 0)
}

func (s *PebbleSuite) TestEnterServicesNoRun(c *C) {
	restore := fakeArgs("pebble", "enter", "--run", "services")
	defer restore()

	exitCode := pebble.PebbleMain()
	c.Check(s.Stderr(), Equals, "error: enter: cannot provide --run before \"services\" subcommand\n")
	c.Check(s.Stdout(), Equals, "")
	c.Check(exitCode, Equals, 1)
}

func (s *PebbleSuite) TestEnterExecListDir(c *C) {
	files := []string{"foo", "bar", "baz"}
	for _, file := range files {
		path := filepath.Join(s.pebbleDir, file)
		if err := ioutil.WriteFile(path, []byte{}, 0644); err != nil {
			panic(err)
		}
	}

	restore := fakeArgs("pebble", "enter", "exec", "ls", s.pebbleDir)
	defer restore()

	exitCode := pebble.PebbleMain()
	c.Check(s.Stderr(), Equals, "")
	c.Check(s.Stdout(), Equals, "bar\nbaz\nfoo\n")
	c.Check(exitCode, Equals, 0)
}

func (s *PebbleSuite) TestEnterExecReadServiceOutputFile(c *C) {
	writeMessageServices(s)

	script := `
		sleep 1
		cd $1
		cat msg1
		cat msg2
	`
	cmd := []string{"pebble", "enter", "--run", "exec", "--",
		"bash", "-c", script, "bash", s.pebbleDir}
	restore := fakeArgs(cmd...)
	defer restore()

	exitCode := pebble.PebbleMain()
	// stderr is written to stdout buffer because of "combine stderr" mode,
	// see cmd/pebble/cmd_exec.go:163
	c.Check(s.Stderr(), Equals, "")
	c.Check(s.Stdout(), Equals, "foo\ncat: msg2: No such file or directory\n")
	c.Check(exitCode, Equals, 1)
}

func (s *PebbleSuite) TestEnterExecCommandHelpOption(c *C) {
	cmd := []string{"pebble", "enter", "exec", "--help"}
	restore := fakeArgs(cmd...)
	defer restore()

	exitCode := pebble.PebbleMain()
	// stderr is written to stdout buffer because of "combine stderr" mode,
	// see cmd/pebble/cmd_exec.go:163
	c.Check(s.Stderr(), Equals, "")
	stdout := s.Stdout()
	c.Check(stdout, Matches, "^(?s)Usage:\n  pebble exec \\[exec-OPTIONS\\] <command>\n.*")
	c.Check(stdout, Matches, "(?s).*\\bThe exec command runs a remote command and waits for it to finish\\..*")
	c.Check(exitCode, Equals, 0)
}

func (s *PebbleSuite) TestEnterHelpCommandHelpOption(c *C) {
	cmd := []string{"pebble", "enter", "help", "--help"}
	restore := fakeArgs(cmd...)
	defer restore()

	exitCode := pebble.PebbleMain()
	// stderr is written to stdout buffer because of "combine stderr" mode,
	// see cmd/pebble/cmd_exec.go:163
	c.Check(s.Stderr(), Equals, "")
	stdout := s.Stdout()
	c.Check(stdout, Matches, "^(?s)Usage:\n  pebble help \\[help-OPTIONS\\] \\[<command>\\.\\.\\.\\]\n.*")
	c.Check(stdout, Matches, "(?s).*\\bThe help command displays information about commands\\..*")
	c.Check(exitCode, Equals, 0)
}

func (s *PebbleSuite) TestEnterHelpCommandExecArg(c *C) {
	cmd := []string{"pebble", "enter", "help", "exec"}
	restore := fakeArgs(cmd...)
	defer restore()

	exitCode := pebble.PebbleMain()
	// stderr is written to stdout buffer because of "combine stderr" mode,
	// see cmd/pebble/cmd_exec.go:163
	c.Check(s.Stderr(), Equals, "")
	stdout := s.Stdout()
	c.Check(stdout, Matches, "^(?s)Usage:\n  pebble exec \\[exec-OPTIONS\\] <command>\n.*")
	c.Check(stdout, Matches, "(?s).*\\bThe exec command runs a remote command and waits for it to finish\\..*")
	c.Check(exitCode, Equals, 0)
}

func (s *PebbleSuite) TestEnterHelpCommandHelpArg(c *C) {
	cmd := []string{"pebble", "enter", "help", "help"}
	restore := fakeArgs(cmd...)
	defer restore()

	exitCode := pebble.PebbleMain()
	// stderr is written to stdout buffer because of "combine stderr" mode,
	// see cmd/pebble/cmd_exec.go:163
	c.Check(s.Stderr(), Equals, "")
	stdout := s.Stdout()
	c.Check(stdout, Matches, "^(?s)Usage:\n  pebble help \\[help-OPTIONS\\] \\[<command>\\.\\.\\.\\]\n.*")
	c.Check(stdout, Matches, "(?s).*\\bThe help command displays information about commands\\..*")
	c.Check(exitCode, Equals, 0)
}
