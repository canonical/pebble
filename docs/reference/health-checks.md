# Health checks

Separate from the service manager, Pebble implements custom "health checks" that can be configured to restart services when they fail.

## Usage

Checks are configured in the layer configuration using the top-level field `checks`:

```yaml
# Optional: A list of health checks managed by this configuration layer.
checks:
    <check name>:
        # Required
        override: merge | replace
        # Optional
        level: alive | ready
        # Optional
        startup: enabled | disabled
        # Optional
        period: <duration>
        # Optional
        timeout: <duration>
        # Optional
        threshold: <failure threshold>

        # HTTP check
        # Only one of "http", "tcp", or "exec" may be specified.
        http:
            # Required
            url: <full URL>
            # Optional
            headers:
                <name>: <value>

        # TCP port
        # Only one of "http", "tcp", or "exec" may be specified.
        tcp:
            # Required
            port: <port number>
            # Optional
            host: <host name>

        # Command execution check
        # Only one of "http", "tcp", or "exec" may be specified.
        exec:
            # Required
            command: <commmand>
            # Optional
            service-context: <service-name>
            # Optional
            environment:
                <name>: <value>
            # Optional
            user: <username>
            # Optional
            user-id: <uid>
            # Optional
            group: <group name>
            # Optional
            group-id: <gid>
            # Optional
            working-dir: <directory>
```

Full details are given in the [layer specification](../reference/layer-specification).

## Options

Each check can be one of three types. The types and their success criteria are:

* `http`: an HTTP `GET` request to the URL specified must return an HTTP 2xx status code
* `tcp`: opening the given TCP port must be successful
* `exec`: executing the specified command must yield a zero exit code

Each check is performed with the specified `period` (the default is 10 seconds apart), and is considered an error if a timeout happens before the check responds -- for example, before the HTTP request is complete or before the command finishes executing.

A check is considered healthy until it's had `threshold` errors in a row (the default is 3). At that point, the check is considered "down", and any associated `on-check-failure` actions will be triggered. When the check succeeds again, the failure count is reset to 0.

To enable Pebble auto-restart behavior based on a check, use the `on-check-failure` map in the service configuration (this is what ties together services and checks). For example, to restart the "server" service when the "test" check fails, use the following:

```
services:
    server:
        override: merge
        on-check-failure:
            # can also be "shutdown", "success-shutdown", or "ignore" (the default)
            test: restart
```

## Examples

Below is an example layer showing the three different types of checks:

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
        startup: disabled
        http:
            url: http://localhost:8080/test
```

## Checks command

You can view check status using the `pebble checks` command. This reports the checks along with their status (`up`, `down`, or `inactive`) and number of failures. For example:

```{terminal}
   :input: pebble checks
Check   Level  Startup   Status    Failures  Change
up      alive  enabled   up        0/1       10
online  ready  enabled   down      1/3       13 (dial tcp 127.0.0.1:8000: connect: connection refused)
test    -      disabled  down      42/3      14 (Get "http://localhost:8080/": dial t... run "pebble tasks 14" for more)
extra   -      disabled  inactive  -         -
```

The "Failures" column shows the current number of failures since the check started failing, a slash, and the configured threshold.

The "Change" column shows the change ID of the [change](changes-and-tasks) driving the check, along with a (possibly-truncated) error message from the last error. Running `pebble tasks <change-id>` will show the change's task, including the last 10 error messages in the task log.

Health checks are implemented using two change kinds:

* `perform-check`: drives the check while it's "up". The change finishes when the number of failures hits the threshold, at which point the change switches to Error status and a `recover-check` change is spawned. Each check failure records a task log.
* `recover-check`: drives the check while it's "down". The change finishes when the check starts succeeding again, at which point the change switches to Done status and a new `perform-check` change is spawned. Again, each check failure records a task log.

When a check is stopped, the active `perform-check` or `recover-check` change is aborted. When a stopped (inactive) check is started, a new `perform-check` change is created for the check.

## Start-checks and stop-checks commands

You can stop one or more checks using the `pebble stop-checks` command. A stopped check shows in the `pebble checks` output as "inactive" status, and the check will no longer be executed until the check is started again. Stopped (inactive) checks appear in check lists but do not contribute to any overall health calculations - they behave as if the check did not exist.

A stopped check that has `startup` set to `enabled` will be started in a `replan` operation and when the layer is first added. Stopped checks can also be manually started via the `pebble start-checks` command.

Checks that have `startup` set to `disabled` will be added in a stopped (inactive) state. These checks will only be started when instructed by a `pebble start-checks` command.

Including a check that is already running in a `start-checks` command, or including a check that is already stopped (inactive) in a `stop-checks` command is always safe and will simply have no effect on the check.

## Health endpoint

If the `--http` option was given when starting `pebble run`, Pebble exposes a `/v1/health` HTTP endpoint that allows a user to query the health of configured checks, optionally filtered by check level with the query string `?level=<level>` This endpoint returns an HTTP 200 status if the checks are healthy, HTTP 502 otherwise.

Stopped (inactive) checks are ignored for health calculations.

Each check can specify a `level` of "alive" or "ready". These have semantic meaning: "alive" means the check or the service it's connected to is up and running; "ready" means it's properly accepting network traffic. These correspond to [Kubernetes "liveness" and "readiness" probes](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/).

The tool running the Pebble server can make use of this, for example, under Kubernetes you could initialize its liveness and readiness probes to hit Pebble's `/v1/health` endpoint with `?level=alive` and `?level=ready` filters, respectively.

If only a "ready" check or only an "alive" check is configured, ready implies alive, and not-alive implies not-ready. If you've configured an "alive" check but no "ready" check, and the "alive" check is unhealthy, `/v1/health?level=ready` will report unhealthy as well, and the Kubernetes readiness probe will act on that.

On the other hand, not-ready does not imply not-alive: if you've configured a "ready" check but no "alive" check, and the "ready" check is unhealthy, `/v1/health?level=alive` will still report healthy.

If there are no checks configured, the `/v1/health` endpoint returns HTTP 200 so the liveness and readiness probes are successful by default. To use this feature, you must explicitly create checks with `level: alive` or `level: ready` in the layer configuration.