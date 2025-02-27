// Copyright (c) 2025 Canonical Ltd
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
	"strings"

	"github.com/canonical/go-flags"
	"gopkg.in/yaml.v3"

	"github.com/canonical/pebble/client"
)

const cmdCheckSummary = "Query the details of a configured health check"
const cmdCheckDescription = `
The check command shows details for a single check in YAML format.
`

type cmdCheck struct {
	client *client.Client

	Refresh bool `long:"refresh"`

	Positional struct {
		Check string `positional-arg-name:"<check>" required:"1"`
	} `positional-args:"yes"`
}

type checkInfo struct {
	Name      string `yaml:"name"`
	Level     string `yaml:"level,omitempty"`
	Startup   string `yaml:"startup"`
	Status    string `yaml:"status"`
	Failures  int    `yaml:"failures"`
	Threshold int    `yaml:"threshold"`
	ChangeID  string `yaml:"change-id,omitempty"`
	Error     string `yaml:"error,omitempty"`
	Logs      string `yaml:"logs,omitempty"`
}

func init() {
	AddCommand(&CmdInfo{
		Name:        "check",
		Summary:     cmdCheckSummary,
		Description: cmdCheckDescription,
		ArgsHelp: map[string]string{
			"--refresh": "Run a check immediately",
		},
		New: func(opts *CmdOptions) flags.Commander {
			return &cmdCheck{client: opts.Client}
		},
	})
}

func (cmd *cmdCheck) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	if cmd.Refresh {
		opts := client.RefreshCheckOptions{
			Name: cmd.Positional.Check,
		}
		res, err := cmd.client.RefreshCheck(&opts)
		if err != nil {
			return err
		}

		checkInfo := checkInfo{
			Name:      res.Info.Name,
			Level:     string(res.Info.Level),
			Startup:   string(res.Info.Startup),
			Status:    string(res.Info.Status),
			Failures:  res.Info.Failures,
			Threshold: res.Info.Threshold,
			ChangeID:  res.Info.ChangeID,
		}
		if res.Error != "" {
			fmt.Println(res.Error)
			checkInfo.Error = res.Error
			logs, err := cmd.taskLogs(checkInfo.ChangeID)
			if err != nil {
				return err
			}
			checkInfo.Logs = logs
		}
		data, err := yaml.Marshal(checkInfo)
		if err != nil {
			return err
		}

		fmt.Fprint(Stdout, string(data))
		return nil
	}

	opts := client.ChecksOptions{
		Names: []string{cmd.Positional.Check},
	}
	checks, err := cmd.client.Checks(&opts)
	if err != nil {
		return err
	}
	if len(checks) == 0 {
		return fmt.Errorf("cannot find check %q", cmd.Positional.Check)
	}

	check := checks[0]
	checkInfo := checkInfo{
		Name:      check.Name,
		Level:     string(check.Level),
		Startup:   string(check.Startup),
		Status:    string(check.Status),
		Failures:  check.Failures,
		Threshold: check.Threshold,
		ChangeID:  check.ChangeID,
	}
	data, err := yaml.Marshal(checkInfo)
	if err != nil {
		return err
	}
	fmt.Fprint(Stdout, string(data))
	return nil
}

func (cmd *cmdCheck) taskLogs(changeID string) (string, error) {
	change, err := cmd.client.Change(changeID)
	if err != nil {
		return "", err
	}
	if len(change.Tasks) < 1 {
		return "", nil
	}
	logs := change.Tasks[0].Log
	if len(logs) < 1 {
		return "", nil
	}

	var allLogs strings.Builder
	for _, logLine := range logs {
		allLogs.WriteString(logLine)
		allLogs.WriteString("\n")
	}
	return allLogs.String(), nil
}

// func (cmd *cmdCheck) taskLogs(changeID string) ([]string, error) {
// 	var logs []string
// 	change, err := cmd.client.Change(changeID)
// 	if err != nil {
// 		return logs, err
// 	}
// 	if len(change.Tasks) < 1 {
// 		return logs, nil
// 	}
// 	logs = change.Tasks[0].Log
// 	return logs, nil
// }
