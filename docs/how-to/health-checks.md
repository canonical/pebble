# How to use health checks

Separate from the service manager, Pebble implements custom "health checks" that can be configured to restart services when they fail.

Each check can be one of three types. The types and their success criteria are:

* `http`: an HTTP `GET` request to the URL specified must return an HTTP 2xx status code
* `tcp`: opening the given TCP port must be successful
* `exec`: executing the specified command must yield a zero exit code

Checks are configured in the layer configuration using the top-level field `checks`. Full details are given in the [layer specification](../reference/layer-specification), but below is an example layer showing the three different types of checks:

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
            # can also be "shutdown", "success-shutdown", or "ignore" (the default)
            test: restart
```

You can view check status using the `pebble checks` command. This reports the checks along with their status (`up` or `down`) and number of failures. For example:

```
$ pebble checks
Check   Level  Status  Failures  Change
up      alive  up      0/1       10
online  ready  down    1/3       13 (dial tcp 127.0.0.1:8000: connect: connection refused)
test    -      down    42/3      14 (Get "http://localhost:8080/": dial t... run "pebble tasks 14" for more)
```

The "Failures" column shows the current number of failures since the check started failing, a slash, and the configured threshold.

The "Change" column shows the change ID of the [change](#changes-and-tasks) driving the check, along with a (possibly-truncated) error message from the last error. Running `pebble tasks <change-id>` will show the change's task, including the last 10 error messages in the task log.

Health checks are implemented using two change kinds:

* `perform-check`: drives the check while it's "up". The change finishes when the number of failures hits the threshold, at which point the change switches to Error status and a `recover-check` change is spawned. Each check failure records a task log.
* `recover-check`: drives the check while it's "down". The change finishes when the check starts succeeding again, at which point the change switches to Done status and a new `perform-check` change is spawned. Again, each check failure records a task log.

## Health endpoint

If the `--http` option was given when starting `pebble run`, Pebble exposes a `/v1/health` HTTP endpoint that allows a user to query the health of configured checks, optionally filtered by check level with the query string `?level=<level>` This endpoint returns an HTTP 200 status if the checks are healthy, HTTP 502 otherwise.

Each check can specify a `level` of "alive" or "ready". These have semantic meaning: "alive" means the check or the service it's connected to is up and running; "ready" means it's properly accepting network traffic. These correspond to [Kubernetes "liveness" and "readiness" probes](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/).

The tool running the Pebble server can make use of this, for example, under Kubernetes you could initialize its liveness and readiness probes to hit Pebble's `/v1/health` endpoint with `?level=alive` and `?level=ready` filters, respectively.

Ready implies alive, and not-alive implies not-ready. If you've configured an "alive" check but no "ready" check, and the "alive" check is unhealthy, `/v1/health?level=ready` will report unhealthy as well, and the Kubernetes readiness probe will act on that.

If there are no checks configured, the `/v1/health` endpoint returns HTTP 200 so the liveness and readiness probes are successful by default. To use this feature, you must explicitly create checks with `level: alive` or `level: ready` in the layer configuration.