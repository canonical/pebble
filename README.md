# The Pebble service manager

_Take control of your internal daemons!_

**Pebble** helps you to orchestrate a set of local service processes as an organized set.
It resembles well known tools such as _supervisord_, _runit_, or _s6_, in that it can
easily manage non-system processes independently from the system services, but it was
designed with unique features that help with more specific use cases.

  - [General model](#general-model)
  - [Layer configuration examples](#layer-configuration-examples)
  - [Running pebble](#running-pebble)
  - [Layer specification](#layer-specification)
  - [API and clients](#api-and-clients)
  - [Roadmap/TODO](#roadmap--todo)
  - [Hacking / Development](#hacking--development)
  - [Contributing](#contributing)

## General model

Pebble is organized as a single binary that works as a daemon and also as a
client to itself. When the daemon runs it loads its own configuration from the
`$PEBBLE` directory, as defined in the environment, and also writes down in
that same directory its state and Unix sockets for communication. If that variable
is not defined, Pebble will attempt to look for its configuration from a default
system-level setup at `/var/lib/pebble/default`. Using that directory is encouraged
for whole-system setup such as when using Pebble to control services in a container.

The `$PEBBLE` directory must contain a `layers/` subdirectory that holds a stack of
configuration files with names similar to `001-base-layer.yaml`, where the digits define
the order of the layer and the following label uniquely identifies it. Each
layer in the stack sits above the former one, and has the chance to improve or
redefine the service configuration as desired.

## Layer configuration examples

This is a complete example of the current [configuration format](#layer-specification):

```yaml
summary: Simple layer

description: |
    A better description for a simple layer.

services:
    srv1:
        override: replace
        summary: Service summary
        command: cmd arg1 "arg2a arg2b"
        startup: enabled
        after:
            - srv2
        before:
            - srv3
        requires:
            - srv2
            - srv3
        environment:
            VAR1: val1
            VAR2: val2
            VAR3: val3
        user: bob
        group: staff

    srv2:
        override: replace
        startup: enabled
        command: cmd
        before:
            - srv3

    srv3:
        override: replace
        command: cmd
```

Some details worth highlighting:

  - The `startup` option can be `enabled` or `disabled`.
  - There is the `override` field (for now required) which defines whether this 
entry _overrides_ the previous service of the same name (if any - missing is 
okay), or merges with it.
  - The optional `user` field allows starting a service with a different user
    than the one Pebble was started with. The `group` field is similar but for
    a group name (it is optional even if `user` is specified).

### Layer override example

Any of the fields can be replaced individually in a merged service configuration.
To illustrate, here is a sample override layer that might sit atop the one above:

```yaml
summary: Simple override layer

services:
    srv1:
        override: merge
        environment:
            VAR3: val3
        after:
            - srv4
        before:
            - srv5

    srv2:
        override: replace
        summary: Replaced service
        startup: disabled
        command: cmd

    srv4:
        override: replace
        command: cmd
        startup: enabled

    srv5:
        override: replace
        command: cmd
```

## Running pebble

If pebble is installed and the `$PEBBLE` directory is set up, running it is easy:

    $ pebble run

This will start the pebble daemon itself, and start all default services as well. Then
other pebble commands may be used to interact with the running daemon.

For example, to see any recent changes, for this or previous runs, use:

    $ pebble changes

And start or stop a specific service with:

    $ pebble start <name1> [<name2> ...]
    $ pebble stop  <name1> [<name2> ...]

## Layer specification

```yaml
# (Optional) A short one line summary of the layer
summary: <summary>

# (Optional) A full description of the configuration layer
description: |
    <description>

# (Optional) A list of services managed by this configuration layer
services:

    <service name>:

        # (Required) Control how this service definition is combined with any
        # other pre-existing definition with the same name in the Pebble plan.
        #
        # The value 'merge' will ensure that values in this layer specification
        # are merged over existing definitions, whereas 'replace' will entirely
        # override the existing service spec in the plan with the same name.
        override: merge | replace

        # (Required in combined layer) The command to run the service. The
        # command is executed directly, not interpreted by a shell.
        #
        # Example: /usr/bin/somecommand -b -t 30
        command: <commmand>

        # (Optional) A short summary of the service.
        summary: <summary>

        # (Optional) A detailed description of the service.
        description: |
            <description>

        # (Optional) Control whether the service is started automatically when
        # Pebble starts. Default is "disabled".
        startup: enabled | disabled

        # (Optional) A list of other services in the plan that this service
        # should start after.
        after:
            - <other service name>

        # (Optional) A list of other services in the plan that this service
        # should start before.
        before:
            - <other service name>

        # (Optional) A list of other services in the plan that this service
        # requires in order to start correctly.
        requires:
            - <other service name>

        # (Optional) A list of key/value pairs defining environment variables
        # that should be set in the context of the process.
        environment:
            <env var name>: <env var value>

        # (Optional) Username for starting service as a different user. It is
        # an error if the user doesn't exist.
        user: <username>

        # (Optional) User ID for starting service as a different user. If both
        # user and user-id are specified, the user's UID must match user-id.
        user-id: <uid>

        # (Optional) Group name for starting service as a different user. It is
        # an error if the group doesn't exist.
        group: <group name>

        # (Optional) Group ID for starting service as a different user. If both
        # group and group-id are specified, the group's GID must match group-id.
        group-id: <gid>

        # (Optional) Defines what happens when the service exits with a zero
        # exit code. Possible values are: "restart" (default) which restarts
        # the service after the backoff delay, "shutdown" which shuts down and
        # exits the Pebble server, and "ignore" which does nothing further.
        on-success: restart | shutdown | ignore

        # (Optional) Defines what happens when the service exits with a nonzero
        # exit code. Possible values are: "restart" (default) which restarts
        # the service after the backoff delay, "shutdown" which shuts down and
        # exits the Pebble server, and "ignore" which does nothing further.
        on-failure: restart | shutdown | ignore

        # (Optional) Defines what happens when each of the named health checks
        # fail. Possible values are: "restart" (default) which restarts
        # the service once, "shutdown" which shuts down and exits the Pebble
        # server, and "ignore" which does nothing further.
        on-check-failure:
            <check name>: restart | shutdown | ignore

        # (Optional) Initial backoff delay for the "restart" exit action.
        # Default is half a second ("500ms").
        backoff-delay: <duration>

        # (Optional) After each backoff, the backoff delay is multiplied by
        # this factor to get the next backoff delay. Must be greater than or
        # equal to one. Default is 2.0.
        backoff-factor: <factor>

        # (Optional) Limit for the backoff delay: when multiplying by
        # backoff-factor to get the next backoff delay, if the result is
        # greater than this value, it is capped to this value. Default is
        # half a minute ("30s").
        backoff-limit: <duration>


# (Optional) A list of health checks managed by this configuration layer.
checks:

    <check name>:

        # (Required) Control how this check definition is combined with any
        # other pre-existing definition with the same name in the Pebble plan.
        #
        # The value 'merge' will ensure that values in this layer specification
        # are merged over existing definitions, whereas 'replace' will entirely
        # override the existing check spec in the plan with the same name.
        override: merge | replace

        # (Optional) Check level, which can be used for filtering checks when
        # calling the checks API or health endpoint.
        #
        # For the health endpoint, ready implies alive. In other words, if all
        # the "ready" checks are succeeding and there are no "alive" checks,
        # the /v1/health API will return success for level=alive.
        level: alive | ready

        # (Optional) Check is run every time this period (time interval)
        # elapses. Must not be zero. Default is "10s".
        period: <duration>

        # (Optional) If this time elapses before a single check operation has
        # finished, it is cancelled and considered an error. Must not be less
        # than the period, and must not be zero. Default is "3s".
        timeout: <duration>

        # (Optional) Number of times in a row the check must error to be
        # considered a failure (which triggers the on-check-failure action).
        # Default 3.
        threshold: <failure threshold>

        # Configures an HTTP check, which is successful if a GET to the
        # specified URL returns a 20x status code.
        #
        # Only one of "http", "tcp", or "exec" may be specified.
        http:
            # (Required) URL to fetch, for example "https://example.com/foo".
            url: <full URL>

            # (Optional) Map of HTTP headers to send with the request.
            headers:
                <name>: <value>

        # Configures a TCP port check, which is successful if the specified
        # TCP port is listening and we can successfully open it. Nothing is
        # sent to the port.
        #
        # Only one of "http", "tcp", or "exec" may be specified.
        tcp:
            # (Required) Port number to open.
            port: <port number>

            # (Optional) Host name or IP address to use. Default is "localhost".
            host: <host name>

        # Configures a command execution check, which is successful if running
        # the specified command returns a zero exit code.
        #
        # Only one of "http", "tcp", or "exec" may be specified.
        exec:
            # (Required) Command line to execute. The command is executed
            # directly, not interpreted by a shell.
            command: <commmand>

            # (Optional) A list of key/value pairs defining environment
            # variables that should be set when running the command.
            environment:
                <name>: <value>

            # (Optional) Username for starting command as a different user. It
            # is an error if the user doesn't exist.
            user: <username>

            # (Optional) User ID for starting command as a different user. If
            # both user and user-id are specified, the user's UID must match
            # user-id.
            user-id: <uid>

            # (Optional) Group name for starting command as a different user.
            # It is an error if the group doesn't exist.
            group: <group name>

            # (Optional) Group ID for starting command as a different user. If
            # both group and group-id are specified, the group's GID must
            # match group-id.
            group-id: <gid>

            # (Optional) Working directory to run command in.
            working-dir: <directory>
```

## API and clients

The Pebble daemon exposes an API (HTTP over a Unix socket) to allow remote clients to interact with the daemon. It can start and stop services, add configuration layers the plan, and so on. There is currently no official documentation for the API (apart from the [code itself](https://github.com/canonical/pebble/blob/master/internal/daemon/api.go)!); most users will interact with it via the Pebble command line interface or the Go or Python client.

The [Go client](https://pkg.go.dev/github.com/canonical/pebble/client) is used by the CLI to connect to the Pebble API. You can use this as follows:

```go
pebble, err := client.New(&client.Config{Socket: ".pebble.socket"})
if err != nil {
    return err
}
files, err := pebble.Start(&client.ServiceOptions{Names: []string{"srv1"}})
if err != nil {
    return err
}
```

We try to never change the underlying API itself in a backwards-incompatible way, however, we may sometimes change the Go client in backwards-incompatible ways.

In addition to the Go client, there's also a [Python client](https://github.com/canonical/operator/blob/master/ops/pebble.py) for the Pebble API that's part of the Python Operator Framework used by Juju charms ([documentation here](https://juju.is/docs/sdk/pebble)).

## Roadmap / TODO

This is a preview of what Pebble is becoming. Please keep that in mind while you
explore.

Here are some of the things coming soon:

  - [x] Support `$PEBBLE_SOCKET` and default `$PEBBLE` to `/var/lib/pebble/default`
  - [x] Define and enforce convention for layer names
  - [x] Dynamic layer support over the API
  - [x] Configuration retrieval commands to investigate current settings
  - [x] Status command that displays active services and their current status
  - [x] General system modification commands (writing configuration files, etc)
  - [x] Better log caching and retrieval support
  - [x] Consider showing unified log as output of `pebble run` (use `-v`)
  - [x] Automatically restart services that fail
  - [x] Support for custom health checks (HTTP, TCP, command)
  - [ ] Automatically remove (double) timestamps from logs
  - [ ] Improve signal handling, e.g., sending SIGHUP to a service
  - [ ] Terminate all services before exiting run command
  - [ ] More tests for existing CLI commands

## Hacking / Development

See [HACKING.md](HACKING.md) for information on how to run and hack on the Pebble codebase during development. In short, use `go run ./cmd/pebble`.

## Contributing

We welcome quality external contributions. We have good unit tests for much of the code, and a thorough code review process. Please note that unless it's a trivial fix, it's generally worth opening an issue to discuss before submitting a pull request.

Before you contribute a pull request you should sign the [Canonical contributor agreement](https://ubuntu.com/legal/contributors) -- it's the easiest way for you to give us permission to use your contributions.

## Have fun!

... and enjoy the rest of the year!
