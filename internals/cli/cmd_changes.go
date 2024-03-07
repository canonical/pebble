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
	"fmt"
	"regexp"
	"sort"

	"github.com/canonical/go-flags"

	"github.com/canonical/pebble/client"
	"github.com/canonical/pebble/cmd"
)

const cmdChangesSummary = "List system changes"
const cmdChangesDescription = `
The changes command displays a summary of system changes performed recently.
`

type cmdChanges struct {
	client *client.Client

	timeMixin
	Positional struct {
		Service string `positional-arg-name:"<service>"`
	} `positional-args:"yes"`
}

const cmdTasksSummary = "List a change's tasks"
const cmdTasksDescription = `
The tasks command displays a summary of tasks associated with an individual
change that happened recently.
`

type cmdTasks struct {
	client *client.Client

	timeMixin
	changeIDMixin
}

func init() {
	AddCommand(&CmdInfo{
		Name:        "changes",
		Summary:     cmdChangesSummary,
		Description: cmdChangesDescription,
		ArgsHelp:    timeArgsHelp,
		New: func(opts *CmdOptions) flags.Commander {
			return &cmdChanges{client: opts.Client}
		},
	})
	AddCommand(&CmdInfo{
		Name:        "tasks",
		Summary:     cmdTasksSummary,
		Description: cmdTasksDescription,
		ArgsHelp:    merge(changeIDMixinArgsHelp, timeArgsHelp),
		New: func(opts *CmdOptions) flags.Commander {
			return &cmdTasks{client: opts.Client}
		},
	})
}

type changesByTime []*client.Change

func (s changesByTime) Len() int           { return len(s) }
func (s changesByTime) Less(i, j int) bool { return s[i].SpawnTime.Before(s[j].SpawnTime) }
func (s changesByTime) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }

var allDigits = regexp.MustCompile(`^[0-9]+$`).MatchString

func queryChanges(cli *client.Client, opts *client.ChangesOptions) ([]*client.Change, error) {
	chgs, err := cli.Changes(opts)
	if err != nil {
		return nil, err
	}
	if err := warnMaintenance(cli); err != nil {
		return nil, err
	}
	return chgs, nil
}

func (c *cmdChanges) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	if allDigits(c.Positional.Service) {
		return fmt.Errorf(`'%s changes' command expects a service name, try '%s tasks %s'`, cmd.ProgramName, cmd.ProgramName, c.Positional.Service)
	}

	if c.Positional.Service == "everything" {
		fmt.Fprintln(Stdout, "Yes, yes it does.")
		return nil
	}

	opts := client.ChangesOptions{
		ServiceName: c.Positional.Service,
		Selector:    client.ChangesAll,
	}

	changes, err := queryChanges(c.client, &opts)
	if err != nil {
		return err
	}

	if len(changes) == 0 {
		return fmt.Errorf("no changes found")
	}

	sort.Sort(changesByTime(changes))

	w := tabWriter()

	fmt.Fprintf(w, "ID\tStatus\tSpawn\tReady\tSummary\n")
	for _, chg := range changes {
		spawnTime := c.fmtTime(chg.SpawnTime)
		readyTime := c.fmtTime(chg.ReadyTime)
		if chg.ReadyTime.IsZero() {
			readyTime = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", chg.ID, chg.Status, spawnTime, readyTime, chg.Summary)
	}

	w.Flush()
	fmt.Fprintln(Stdout)

	return nil
}

func (c *cmdTasks) Execute([]string) error {
	chid, err := c.GetChangeID(c.client)
	if err != nil {
		if err == noChangeFoundOK {
			return nil
		}
		return err
	}

	return c.showChange(chid)
}

func queryChange(cli *client.Client, chid string) (*client.Change, error) {
	chg, err := cli.Change(chid)
	if err != nil {
		return nil, err
	}
	if err := warnMaintenance(cli); err != nil {
		return nil, err
	}
	return chg, nil
}

func (c *cmdTasks) showChange(chid string) error {
	chg, err := queryChange(c.client, chid)
	if err != nil {
		return err
	}

	w := tabWriter()

	fmt.Fprintf(w, "Status\tSpawn\tReady\tSummary\n")
	for _, t := range chg.Tasks {
		spawnTime := c.fmtTime(t.SpawnTime)
		readyTime := c.fmtTime(t.ReadyTime)
		if t.ReadyTime.IsZero() {
			readyTime = "-"
		}
		summary := t.Summary
		if t.Status == "Doing" && t.Progress.Total > 1 {
			summary = fmt.Sprintf("%s (%.2f%%)", summary, float64(t.Progress.Done)/float64(t.Progress.Total)*100.0)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", t.Status, spawnTime, readyTime, summary)
	}

	w.Flush()

	for _, t := range chg.Tasks {
		if len(t.Log) == 0 {
			continue
		}
		fmt.Fprintln(Stdout)
		fmt.Fprintln(Stdout, line)
		fmt.Fprintln(Stdout, t.Summary)
		fmt.Fprintln(Stdout)
		for _, line := range t.Log {
			fmt.Fprintln(Stdout, line)
		}
	}

	fmt.Fprintln(Stdout)

	return nil
}

const line = "......................................................................"

func warnMaintenance(cli *client.Client) error {
	if maintErr := cli.Maintenance(); maintErr != nil {
		msg, err := errorToMessage(maintErr)
		if err != nil {
			return err
		}
		fmt.Fprintf(Stderr, "WARNING: %s\n", msg)
	}
	return nil
}
