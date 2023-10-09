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

package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

type ServiceOptions struct {
	Names []string
}

// AutoStart starts the services makes as "startup: enabled". opts.Names must
// be empty for this call.
func (client *Client) AutoStart(opts *ServiceOptions) (changeID string, err error) {
	changeID, err = client.doMultiServiceAction("autostart", opts.Names)
	return changeID, err
}

// Start starts the services named in opts.Names in dependency order.
func (client *Client) Start(opts *ServiceOptions) (changeID string, err error) {
	changeID, err = client.doMultiServiceAction("start", opts.Names)
	return changeID, err
}

// Stop stops the services named in opts.Names in dependency order.
func (client *Client) Stop(opts *ServiceOptions) (changeID string, err error) {
	changeID, err = client.doMultiServiceAction("stop", opts.Names)
	return changeID, err
}

// Restart stops and then starts the services named in opts.Names in
// dependency order.
func (client *Client) Restart(opts *ServiceOptions) (changeID string, err error) {
	changeID, err = client.doMultiServiceAction("restart", opts.Names)
	return changeID, err
}

// Replan stops and (re)starts the services whose configuration has changed
// since they were started. opts.Names must be empty for this call.
func (client *Client) Replan(opts *ServiceOptions) (changeID string, err error) {
	changeID, err = client.doMultiServiceAction("replan", opts.Names)
	return changeID, err
}

type multiActionData struct {
	Action   string   `json:"action"`
	Services []string `json:"services"`
}

func (client *Client) doMultiServiceAction(actionName string, services []string) (changeID string, err error) {
	action := multiActionData{
		Action:   actionName,
		Services: services,
	}
	data, err := json.Marshal(&action)
	if err != nil {
		return "", fmt.Errorf("cannot marshal multi-service action: %w", err)
	}
	headers := map[string]string{
		"Content-Type": "application/json",
	}

	resp, err := client.doAsync("POST", "/v1/services", nil, headers, bytes.NewBuffer(data), nil)
	if err != nil {
		return "", err
	}
	return resp.ChangeID, nil
}

type ServicesOptions struct {
	// Names is the list of service names to query for. If slice is nil or
	// empty, fetch information for all services.
	Names []string
}

// ServiceInfo holds status information for a single service.
type ServiceInfo struct {
	Name         string         `json:"name"`
	Startup      ServiceStartup `json:"startup"`
	Current      ServiceStatus  `json:"current"`
	CurrentSince time.Time      `json:"current-since"`
}

// ServiceStartup defines the different startup modes for a service.
type ServiceStartup string

const (
	StartupEnabled  ServiceStartup = "enabled"
	StartupDisabled ServiceStartup = "disabled"
)

// ServiceStatus defines the current states for a service.
type ServiceStatus string

const (
	StatusActive   ServiceStatus = "active"
	StatusBackoff  ServiceStatus = "backoff"
	StatusError    ServiceStatus = "error"
	StatusInactive ServiceStatus = "inactive"
)

// Services fetches information about specific services (or all of them),
// ordered by service name.
func (client *Client) Services(opts *ServicesOptions) ([]*ServiceInfo, error) {
	query := url.Values{
		"names": []string{strings.Join(opts.Names, ",")},
	}
	var services []*ServiceInfo
	_, err := client.doSync("GET", "/v1/services", query, nil, nil, &services)
	if err != nil {
		return nil, err
	}
	return services, nil
}
