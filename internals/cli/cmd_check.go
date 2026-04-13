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
	Name         string `yaml:"name"`
	Level        string `yaml:"level,omitempty"`
	Startup      string `yaml:"startup"`
	Status       string `yaml:"status"`
	Successes    *int   `yaml:"successes,omitempty"`
	Failures     int    `yaml:"failures"`
	Threshold    int    `yaml:"threshold"`
	ChangeID     string `yaml:"change-id,omitempty"`
	PrevChangeID string `yaml:"prev-change-id,omitempty"`
	Error        string `yaml:"error,omitempty"`
	Logs         string `yaml:"logs,omitempty"`
}

func init() {
	AddCommand(&CmdInfo{
		Name:        "check",
		Summary:     cmdCheckSummary,
		Description: cmdCheckDescription,
		ArgsHelp: map[string]string{
			"--refresh": "Run the check immediately",
		},
		New: func(opts *CmdOptions) flags.Commander {
			return &cmdCheck{client: opts.Client}
		},
	})
}

func checkInfoFromClient(check client.CheckInfo) checkInfo {
	return checkInfo{
		Name:         check.Name,
		Level:        string(check.Level),
		Startup:      string(check.Startup),
		Status:       string(check.Status),
		Successes:    check.Successes,
		Failures:     check.Failures,
		Threshold:    check.Threshold,
		ChangeID:     check.ChangeID,
		PrevChangeID: check.PrevChangeID,
	}
}

func (cmd *cmdCheck) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	var info checkInfo
	if cmd.Refresh {
		opts := client.RefreshCheckOptions{
			Name: cmd.Positional.Check,
		}
		res, err := cmd.client.RefreshCheck(&opts)
		if err != nil {
			return err
		}

		info = checkInfoFromClient(res.Info)
		info.Error = res.Error
	} else {
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
		info = checkInfoFromClient(*checks[0])
	}

	if info.Failures > 0 || info.Error != "" {
		if info.ChangeID != "" {
			logs, err := cmd.taskLogs(info.ChangeID)
			if err != nil {
				return fmt.Errorf("cannot get task logs for change %s: %w", info.ChangeID, err)
			}
			if logs == "" && info.PrevChangeID != "" {
				logs, err = cmd.taskLogs(info.PrevChangeID)
				if err != nil {
					return fmt.Errorf("cannot get task logs for change %s: %w", info.PrevChangeID, err)
				}
			}
			info.Logs = logs
		}
	}
	data, err := yaml.Marshal(info)
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
