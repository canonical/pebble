# The Pebble service manager

[![pebble](https://snapcraft.io/pebble/badge.svg)](https://snapcraft.io/pebble)

_Take control of your internal daemons!_

**Pebble** helps you to orchestrate a set of local service processes as an organized set.
It resembles well known tools such as _supervisord_, _runit_, or _s6_, in that it can
easily manage non-system processes independently from the system services, but it was
designed with unique features that help with more specific use cases.

  - [General model](#general-model)
  - [Layer configuration examples](#layer-configuration-examples)
  - [Using Pebble](#using-pebble)
  - [Container usage](#container-usage)
  - [Layer specification](#layer-specification)
  - [API and clients](#api-and-clients)
  - [Roadmap/TODO](#roadmap--todo)
  - [Hacking / Development](#hacking--development)
  - [Contributing](#contributing)

## General model

Pebble is organized as a single binary that works as a daemon and also as a
client to itself. When the daemon runs it loads its own configuration from the
`$PEBBLE` directory, as defined in the environment, and also records in
that same directory its state and unix sockets for communication. If that variable
is not defined, Pebble will attempt to look for its configuration from a default
system-level setup at `/var/lib/pebble/default`. Using that directory is encouraged
for whole-system setup such as when using Pebble to control services in a container.

The `$PEBBLE` directory must contain a `layers/` subdirectory that holds a stack of
configuration files with names similar to `001-base-layer.yaml`, where the digits define
the order of the layer and the following label uniquely identifies it. Each
layer in the stack sits above the former one, and has the chance to improve or
redefine the service configuration as desired.

## Layer configuration examples

Below is an example of the current configuration format.
For full details of all fields, see the [complete layer specification](#layer-specification).

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

The `override` field (which is required) defines whether this 
entry _overrides_ the previous service of the same name (if any),
or merges with it. See the [full layer specification](#layer-specification)
for more details.

### Layer override example

Any of the fields can be replaced individually in a merged service configuration.
To illustrate, here is a sample override layer that might sit on top of the one above:

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

## Using Pebble

To install the latest version of Pebble, run the following command (we don't currently
ship binaries, so you must first [install Go](https://go.dev/doc/install)):
```
go install github.com/canonical/pebble/cmd/pebble@latest
```

Pebble is invoked using `pebble <command>`. To get more information:

* To see a help summary, type `pebble -h`.
* To see a short description of all commands, type `pebble help --all`.
* To see details for one command, type `pebble help <command>` or `pebble <command> -h`.

A few of the commands that need more explanation are detailed below.

### Running the daemon (server)

If Pebble is installed and the `$PEBBLE` directory is set up, running the daemon is easy:

```
$ pebble run
2022-10-26T01:18:26.904Z [pebble] Started daemon.
2022-10-26T01:18:26.921Z [pebble] POST /v1/services 15.53132ms 202
2022-10-26T01:18:26.921Z [pebble] Started default services with change 50.
2022-10-26T01:18:26.936Z [pebble] Service "srv1" starting: sleep 300
```

This will start the Pebble daemon itself, as well as starting all the services that
are marked as `startup: enabled` (if you don't want that, use `--hold`). Then
other Pebble commands may be used to interact with the running daemon, for example,
in another terminal window.

To provide additional arguments to a service, use `--args <service> <args> ...`.
If the `command` field in the service's plan has a `[ <default-arguments...> ]`
list, the `--args` arguments will replace the defaults. If not, they will be
appended to the command.

To indicate the end of an `--args` list, use a `;` (semicolon) terminator,
which must be backslash-escaped if used in the shell. The terminator
may be omitted if there are no other Pebble options that follow.

For example:

```
# Start the daemon and pass additional arguments to "myservice".
$ pebble run --args myservice --verbose --foo "multi str arg"

# Use args terminator to pass --hold to Pebble at the end of the line.
$ pebble run --args myservice --verbose \; --hold

# Start the daemon and pass arguments to multiple services.
$ pebble run --args myservice1 --arg1 \; --args myservice2 --arg2
```

To override the default configuration directory, set the `PEBBLE` environment variable when running:

```
$ export PEBBLE=~/pebble
pebble run
2022-10-26T01:18:26.904Z [pebble] Started daemon.
...
```

### Viewing, starting, and stopping services

You can view the status of one or more services by using `pebble services`:

```
$ pebble services srv1       # show status of a single service
Service  Startup  Current
srv1     enabled  active

$ pebble services            # show status of all services
Service  Startup   Current
srv1     enabled   active
srv2     disabled  inactive
```

The "Startup" column shows whether this service is automatically started when Pebble starts ("enabled" means auto-start, "disabled" means don't auto-start).

The "Current" column shows the current status of the service, and can be one of the following:

* `active`: starting or running
* `inactive`: not yet started, being stopped, or stopped
* `backoff`: in a [backoff-restart loop](#service-auto-restart)
* `error`: in an error state

To start specific services, type `pebble start` followed by one or more service names:

```
$ pebble start srv1 srv2  # start two services (and any dependencies)
```

When starting a service, Pebble executes the service's `command`, and waits 1 second to ensure the command doesn't exit too quickly. Assuming the command doesn't exit within that time window, the start is considered successful, otherwise `pebble start` will exit with an error.

Similarly, to stop specific services, use `pebble stop` followed by one or more service names:

```
$ pebble stop srv1        # stop one service
```

When stopping a service, Pebble sends SIGTERM to the service's process group, and waits up to 5 seconds. If the command hasn't exited within that time window, Pebble sends SIGKILL to the service's process group and waits up to 5 more seconds. If the command exits within that 10-second time window, the stop is considered successful, otherwise `pebble stop` will exit with an error.

### Updating and restarting services

When you update service configuration (by adding a layer), the services changed won't be automatically restarted. To restart them and bring the service state in sync with the new configuration, use `pebble replan`.

The "replan" operation restarts `startup: enabled` services whose configuration have changed between when they started and now; if the configuration hasn't changed, replan does nothing. Replan also starts `startup: enabled` services that have not yet been started.

Here is an example, where `srv1` is a service that has `startup: enabled`, and `srv2` does not:

```
$ pebble replan
2023-04-25T15:06:50+02:00 INFO Service "srv1" already started.
$ pebble add lay1 layer.yaml  # update srv1 config
Layer "lay1" added successfully from "layer.yaml"
$ pebble replan
Stop service "srv1"
Start service "srv1"
$ pebble add lay2 layer.yaml  # change srv2 to "startup: enabled"
Layer "lay2" added successfully from "layer.yaml"
$ pebble replan
2023-04-25T15:11:22+02:00 INFO Service "srv1" already started.
Start service "srv2"
```

If you want to force a service to restart even if its service configuration hasn't changed, use `pebble restart <service>`.

### Service dependencies

Pebble takes service dependencies into account when starting and stopping services. When Pebble starts a service, it also starts the services which that service depends on (configured with `required`). Conversely, when stopping a service, Pebble also stops services which depend on that service.

For example, if service `nginx` requires `logger`, `pebble start nginx` will start both `nginx` and `logger` (in an undefined order). Running `pebble stop logger` will stop both `nginx` and `logger`; however, running `pebble stop nginx` will only stop `nginx` (`nginx` depends on `logger`, not the other way around).

When multiple services need to be started together, they're started in order according to the `before` and `after` configuration, waiting 1 second for each to ensure the command doesn't exit too quickly. The `before` option is a list of services that this service must start before (it may or may not `require` them). Or if it's easier to specify this ordering the other way around, `after` is a list of services that this service must start after.

Note that currently, `before` and `after` are of limited usefulness, because Pebble only waits 1 second before moving on to start the next service, with no additional checks that the previous service is operating correctly.

If the configuration of `requires`, `before`, and `after` for a service results in a cycle or "loop", an error will be returned when attempting to start or stop the service.

### Service auto-restart

Pebble's service manager automatically restarts services that exit unexpectedly. By default, this is done whether the exit code is zero or non-zero, but you can change this using the `on-success` and `on-failure` fields in a configuration layer. The possible values for these fields are:

* `restart`: restart the service and enter a restart-backoff loop (the default behaviour).
* `shutdown`: shut down and exit the Pebble daemon
* `ignore`: ignore the service exiting and do nothing further

In `restart` mode, the first time a service exits, Pebble waits the `backoff-delay`, which defaults to half a second. If the service exits again, Pebble calculates the next backoff delay by multiplying the current delay by `backoff-factor`, which defaults to 2.0 (doubling). The increasing delay is capped at `backoff-limit`, which defaults to 30 seconds.

The `backoff-limit` value is also used as a "backoff reset" time. If the service stays running after a restart for `backoff-limit` seconds, the backoff process is reset and the delay reverts to `backoff-delay`.

### Health checks

Separate from the service manager, Pebble implements custom "health checks" that can be configured to restart services when they fail.

Each check can be one of three types. The types and their success criteria are:

* `http`: an HTTP `GET` request to the URL specified must return an HTTP 2xx status code
* `tcp`: opening the given TCP port must be successful
* `exec`: executing the specified command must yield a zero exit code

Checks are configured in the layer configuration using the top-level field `checks`. Full details are given in the [layer specification](#layer-specification), but below is an example layer showing the three different types of checks:

```
checks:
    up:
        override: replace
        level: alive
        period: 30s
        threshold: 1  # an aggressive threshold
        exec:
            command: service nginx status

    online:
        override: replace
        level: ready
        tcp:
            port: 8080

    test:
        override: replace
        http:
            url: http://localhost:8080/test
```

Each check is performed with the specified `period` (the default is 10 seconds apart), and is considered an error if a timeout happens before the check responds -- for example, before the HTTP request is complete or before the command finishes executing.

A check is considered healthy until it's had `threshold` errors in a row (the default is 3). At that point, the check is considered "down", and any associated `on-check-failure` actions will be triggered. When the check succeeds again, the failure count is reset to 0.

To enable Pebble auto-restart behavior based on a check, use the `on-check-failure` map in the service configuration (this is what ties together services and checks). For example, to restart the "server" service when the "test" check fails, use the following:

```
services:
    server:
        override: merge
        on-check-failure:
            test: restart   # can also be "shutdown" or "ignore" (the default)
```

You can view check status using the `pebble checks` command. This reports the checks along with their status (`up` or `down`) and number of failures. For example:

```
$ pebble checks
Check   Level  Status  Failures
up      alive  up      0/1
online  ready  down    1/3
test    -      down    42/3
```

The "Failures" column shows the current number of failures since the check started failing, a slash, and the configured threshold.

If the `--http` option was given when starting `pebble run`, Pebble exposes a `/v1/health` HTTP endpoint that allows a user to query the health of configured checks, optionally filtered by check level with the query string `?level=<level>` This endpoint returns an HTTP 200 status if the checks are healthy, HTTP 502 otherwise.

Each check can specify a `level` of "alive" or "ready". These have semantic meaning: "alive" means the check or the service it's connected to is up and running; "ready" means it's properly accepting network traffic. These correspond to [Kubernetes "liveness" and "readiness" probes](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/).

The tool running the Pebble server can make use of this, for example, under Kubernetes you could initialize its liveness and readiness probes to hit Pebble's `/v1/health` endpoint with `?level=alive` and `?level=ready` filters, respectively.

Ready implies alive, and not-alive implies not-ready. If you've configured an "alive" check but no "ready" check, and the "alive" check is unhealthy, `/v1/health?level=ready` will report unhealthy as well, and the Kubernetes readiness probe will act on that.

If there are no checks configured, the `/v1/health` endpoint returns HTTP 200 so the liveness and readiness probes are successful by default. To use this feature, you must explicitly create checks with `level: alive` or `level: ready` in the layer configuration.

### Changes and tasks

When Pebble performs a (potentially invasive or long-running) operation such as starting or stopping a service, it records a "change" object with one or more "tasks" in it. The daemon records this state in a JSON file on disk at `$PEBBLE/.pebble.state`.

To see recent changes, for this or previous server runs, use `pebble changes`. You might see something like this:

```
$ pebble changes
ID  Status  Spawn                Ready                Summary
1   Done    today at 14:33 NZDT  today at 14:33 NZDT  Autostart service "srv1"
2   Done    today at 15:26 NZDT  today at 15:26 NZDT  Start service "srv2"
3   Done    today at 15:26 NZDT  today at 15:26 NZDT  Stop service "srv1" and 1 more
```

To drill down and see the tasks that make up a change, use `pebble tasks <change-id>`:

```
$ pebble tasks 3
Status  Spawn                Ready                Summary
Done    today at 15:26 NZDT  today at 15:26 NZDT  Stop service "srv1"
Done    today at 15:26 NZDT  today at 15:26 NZDT  Stop service "srv2"
```

### Logs

The daemon's service manager stores the most recent stdout and stderr from each service, using a 100KB ring buffer per service. Each log line is prefixed with an RFC-3339 timestamp and the `[service-name]` in square brackets.

Logs are viewable via the logs API or using `pebble logs`, for example:

```
$ pebble logs
2022-11-14T01:35:06.979Z [srv1] Log 0 from srv1
2022-11-14T01:35:08.041Z [srv2] Log 0 from srv2
2022-11-14T01:35:09.982Z [srv1] Log 1 from srv1
```

To view existing logs and follow (tail) new output, use `-f` (press Ctrl-C to exit):

```
$ pebble logs -f
2022-11-14T01:37:56.936Z [srv1] Log 0 from srv1
2022-11-14T01:37:57.978Z [srv2] Log 0 from srv2
2022-11-14T01:37:59.939Z [srv1] Log 1 from srv1
^C
```

You can output logs in JSON Lines format, using `--format=json`:

```
$ pebble logs --format=json
{"time":"2022-11-14T01:39:10.886Z","service":"srv1","message":"Log 0 from srv1"}
{"time":"2022-11-14T01:39:11.943Z","service":"srv2","message":"Log 0 from srv2"}
{"time":"2022-11-14T01:39:13.889Z","service":"srv1","message":"Log 1 from srv1"}
```

If you want to also write service logs to Pebble's own stdout, run the daemon with `--verbose`:

```
$ pebble run --verbose
2022-10-26T01:41:32.805Z [pebble] Started daemon.
2022-10-26T01:41:32.835Z [pebble] POST /v1/services 29.743632ms 202
2022-10-26T01:41:32.835Z [pebble] Started default services with change 7.
2022-10-26T01:41:32.849Z [pebble] Service "srv1" starting: python3 -u /path/to/srv1.py
2022-10-26T01:41:32.866Z [srv1] Log 0 from srv1
2022-10-26T01:41:35.870Z [srv1] Log 1 from srv1
2022-10-26T01:41:38.873Z [srv1] Log 2 from srv1
...
```

### Log forwarding

Pebble supports forwarding its services' logs to a remote Loki server. In the `log-targets` section of the plan, you can specify destinations for log forwarding, for example:
```yaml
log-targets:
    staging-logs:
        override: merge
        type: loki
        location: http://10.1.77.205:3100/loki/api/v1/push
        services: [all]
    production-logs:
        override: merge
        type: loki
        location: http://my.loki.server.com/loki/api/v1/push
        services: [svc1, svc2]
```

#### Specifying services

For each log target, use the `services` key to specify a list of services to collect logs from. In the above example, the `production-logs` target will collect logs from `svc1` and `svc2`.

Use the special keyword `all` to match all services, including services that might be added in future layers. In the above example, `staging-logs` will collect logs from all services.

To remove a service from a log target when merging, prefix the service name with a minus `-`. For example, if we have a base layer with
```yaml
my-target:
    services: [svc1, svc2]
```
and override layer with
```yaml
my-target:
    services: [-svc1]
    override: merge
```
then in the merged layer, the `services` list will be merged to `[svc1, svc2, -svc1]`, which evaluates left to right as simply `[svc2]`. So `my-target` will collect logs from only `svc2`.

You can also use `-all` to remove all services from the list. For example, adding an override layer with
```yaml
my-target:
    services: [-all]
    override: merge
```
would remove all services from `my-target`, effectively disabling `my-target`. Meanwhile, adding an override layer with
```yaml
my-target:
    services: [-all, svc1]
    override: merge
```
would remove all services and then add `svc1`, so `my-target` would receive logs from only `svc1`.

#### Labels

In the `labels` section, you can specify custom labels to be added to any outgoing logs. These labels may contain `$ENVIRONMENT_VARIABLES` - these will be interpreted in the environment of the corresponding service. Pebble may also add its own default labels (depending on the protocol). For example, given the following plan:
```yaml
services:
  svc1:
    environment:
      OWNER: 'alice'
  svc2:
    environment:
      OWNER: 'bob'

log-targets:
  tgt1:
    type: loki
    labels:
      product: 'juju'
      owner: 'user-$OWNER'
```
the logs from `svc1` will be sent with the following labels:
```yaml
product: juju
owner: user-alice     # env var $OWNER substituted
pebble_service: svc1  # default label for Loki
```
and for svc2, the labels will be
```yaml
product: juju
owner: user-bob       # env var $OWNER substituted
pebble_service: svc2  # default label for Loki
```


### Notices

Pebble includes a subsystem called *notices*, which allows the user to introspect various events that occur in the Pebble server, as well as record custom client events. The server saves notices to disk, so they persist across restarts, and expire after a notice-defined interval.

Each notice is uniquely identified by its *type* and *key* combination, and the notice's count of occurences is incremented every time a notice with that type and key combination occurs.

Each notice records the time it first occurred, the time it last occurred, and the time it last repeated.

A *repeat* happens when a notice occurs with the same type and key as a prior notice, and either the notice has no "repeat after" duration (the default), or the notice happens after the provided "repeat after" interval (since the prior notice). Thus, specifying "repeat after" prevents a notice from appearing again if it happens more frequently than desired.

In addition, a notice records optional *data* (string key-value pairs) from the last occurrence.

These notice types are currently available:

<!-- TODO: * `change-update`: recorded whenever a change is first spawned or its status is updated. The key for this type of notice is the change ID, and the notice's data includes the change `kind`. -->

* `custom`: a custom client notice reported via `pebble notify`. The key and any data is provided by the user. The key must be in the format `mydomain.io/mykey` to ensure well-namespaced notice keys.

<!-- TODO: * `warning`: Pebble warnings are implemented in terms of notices. The key for this type of notice is the human-readable warning message. -->

To record `custom` notices, use `pebble notify`:

```
$ pebble notify example.com/foo
Recorded notice 1
$ pebble notify example.com/foo
Recorded notice 1
$ pebble notify other.com/bar name=value email=john@smith.com  # two data fields
Recorded notice 2
$ pebble notify example.com/foo
Recorded notice 1
```

The `pebble notices` command lists notices not yet acknowledged, ordered by the last-repeated time (oldest first). After it runs, the notices that were shown may then be acknowledged by running `pebble okay`. When a notice repeats (see above), it needs to be acknowledged again.

```
$ pebble notices
ID   Type    Key              First                Repeated             Occ
1    custom  example.com/foo  today at 16:16 NZST  today at 16:16 NZST  3
2    custom  other.com/bar    today at 16:16 NZST  today at 16:16 NZST  1
```

To fetch details about a single notice, use `pebble notice`, which displays the output in YAML format. You can fetch a notice either by ID or by type/key combination.

To fetch the notice with ID "1":

```
$ pebble notice 1
id: "1"
type: custom
key: example.com/foo
first-occurred: 2023-09-15T04:16:09.179395298Z
last-occurred: 2023-09-15T04:16:19.487035209Z
last-repeated: 2023-09-15T04:16:09.179395298Z
occurrences: 3
expire-after: 168h0m0s
```

To fetch the notice with type "custom" and key "other.com/bar":

```
$ pebble notice custom other.com/bar
id: "2"
type: custom
key: other.com/bar
first-occurred: 2023-09-15T04:16:17.180049768Z
last-occurred: 2023-09-15T04:16:17.180049768Z
last-repeated: 2023-09-15T04:16:17.180049768Z
occurrences: 1
last-data:
    name: value
    email: john@smith.com
expire-after: 168h0m0s
```


## Container usage

Pebble works well as a local service manager, but if running Pebble in a separate container, you can use the exec and file management APIs to coordinate with the remote system over the shared unix socket.

### Exec (one-shot commands)

Pebble's "exec" feature allows you to run arbitrary commands on the server. This is intended for short-running programs; the processes started with exec don't use the service manager.

For example, you could use `exec` to run pg_dump and create a PostgreSQL database backup:

```
$ pebble exec pg_dump mydb
--
-- PostgreSQL database dump
--
...
```

The exec feature uses WebSockets under the hood, and allows you to stream stdin to the process, as well as stream stdout and stderr back. When running `pebble exec`, you can specify the working directory to run in (`-w`), environment variables to set (`--env`), and the user and group to run as (`--uid`/`--user` and `--gid`/`--group`).

You can also apply a timeout with `--timeout`, for example:

```
$ pebble exec --timeout 1s -- sleep 3
error: cannot perform the following tasks:
- exec command "sleep" (timed out after 1s: context deadline exceeded)
```

### File management

Pebble provides various API calls and commands to manage files and directories on the server. The simplest way to use these is with the commands below, several of which should be familiar:

```
$ pebble ls <path>              # list file information (like "ls")
$ pebble mkdir <path>           # create a directory (like "mkdir")

# TODO -- the following commands are coming soon
$ pebble rm <path>              # remove a file or directory (like "rm")
$ pebble push <local> <remote>  # copy file to server (like "cp")
$ pebble pull <remote> <local>  # copy file from server (like "cp")
```

## Layer specification

Below is the full specification for a Pebble configuration layer. Layers are added statically using a file in `$PEBBLE/layers`, or dynamically via the layers API or `pebble add`.

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

        # (Required in combined layer) The command to run the service. It is executed
        # directly, not interpreted by a shell, and may be optionally suffixed by default
        # arguments within "[" and "]" which may be overriden via --args.
        # Example: /usr/bin/somedaemon --db=/db/path [ --port 8080 ]
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

        # (Optional) Working directory to run command in. By default, the
        # command is run in the service manager's current directory.
        working-dir: <directory>

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

        # (Optional) The amount of time afforded to this service to handle
        # SIGTERM and exit gracefully before SIGKILL terminates it forcefully.
        # Default is 5 seconds ("5s").
        kill-delay: <duration>

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
        # finished, it is cancelled and considered an error. Must be less
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

            # (Optional) Run the command in the context of this service.
            # Specifically, inherit its environment variables, user/group
            # settings, and working directory. The check's context (the
            # settings below) will override the service's; the check's
            # environment map will be merged on top of the service's.
            service-context: <service-name>

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

            # (Optional) Working directory to run command in. By default, the
            # command is run in the service manager's current directory.
            working-dir: <directory>

# (Optional) A list of remote log receivers, to which service logs can be sent.
log-targets:

  <log target name>:

    # (Required) Control how this log target definition is combined with
    # other pre-existing definitions with the same name in the Pebble plan.
    #
    # The value 'merge' will ensure that values in this layer specification
    # are merged over existing definitions, whereas 'replace' will entirely
    # override the existing target spec in the plan with the same name.
    override: merge | replace

    # (Required) The type of log target, which determines the format in
    # which logs will be sent. The supported types are:
    #
    # - loki: Use the Grafana Loki protocol. A "pebble_service" label is
    #   added automatically, with the name of the Pebble service as its value.
    type: loki

    # (Required) The URL of the remote log target.
    # For Loki, this needs to be the fully-qualified URL of the push API,
    # including the API endpoint, e.g.
    #     http://<ip-address>:3100/loki/api/v1/push
    location: <url>

    # (Optional) A list of services whose logs will be sent to this target.
    # Use the special keyword 'all' to match all services in the plan.
    # When merging log targets, the 'services' lists are appended. Prefix a
    # service name with a minus (e.g. '-svc1') to remove a previously added
    # service. '-all' will remove all services.
    services: [<service names>]

    # (Optional) A list of key/value pairs defining labels which should be set
    # on the outgoing logs. The label values may contain $ENV_VARS, which will
    # be substituted using the environment for the corresponding service.
    labels:
      <label name>: <label value>
```

## API and clients

The Pebble daemon exposes an API (HTTP over a unix socket) to allow remote clients to interact with the daemon. It can start and stop services, add configuration layers the plan, and so on.

There is currently no official documentation for the API at the HTTP level (apart from the [code itself](https://github.com/canonical/pebble/blob/master/internals/daemon/api.go)!); most users will interact with it via the Pebble command line interface or by using the Go or Python clients.

The Go client is used primarily by the CLI, but is importable and can be used by other tools too. See the [reference documentation and examples](https://pkg.go.dev/github.com/canonical/pebble/client) at pkg.go.dev.

We try to never change the underlying HTTP API in a backwards-incompatible way, however, in rare cases we may change the Go client in a backwards-incompatible way.

In addition to the Go client, there's also a [Python client](https://github.com/canonical/operator/blob/master/ops/pebble.py) for the Pebble API that's part of the [`ops` library](https://github.com/canonical/operator) used by Juju charms ([documentation here](https://juju.is/docs/sdk/interact-with-pebble)).

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
  - [x] Terminate all services before exiting run command
  - [x] Log forwarding to Loki
  - [ ] Log forwarding to syslog
  - [ ] [Other in-progress PRs](https://github.com/canonical/pebble/pulls)
  - [ ] [Other requested features](https://github.com/canonical/pebble/issues)

## Hacking / Development

See [HACKING.md](HACKING.md) for information on how to run and hack on the Pebble codebase during development. In short, use `go run ./cmd/pebble`.

## Contributing

We welcome quality external contributions. We have good unit tests for much of the code, and a thorough code review process. Please note that unless it's a trivial fix, it's generally worth opening an issue to discuss before submitting a pull request.

Before you contribute a pull request you should sign the [Canonical contributor agreement](https://ubuntu.com/legal/contributors) -- it's the easiest way for you to give us permission to use your contributions.

## Have fun!

... and enjoy the rest of the year!
