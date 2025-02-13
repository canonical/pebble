# Layer specification

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
        # Pebble starts or performs a 'replan' operation. Default is "disabled".
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
        # exit code. Possible values are:
        #
        # - restart (default): restart the service after the backoff delay
        # - shutdown: shut down and exit the Pebble daemon (with exit code 0)
        # - failure-shutdown: shut down and exit Pebble with exit code 10
        # - ignore: do nothing further
        on-success: restart | shutdown | failure-shutdown | ignore

        # (Optional) Defines what happens when the service exits with a nonzero
        # exit code. Possible values are:
        #
        # - restart (default): restart the service after the backoff delay
        # - shutdown: shut down and exit the Pebble daemon (with exit code 10)
        # - success-shutdown: shut down and exit Pebble with exit code 0
        # - ignore: do nothing further
        on-failure: restart | shutdown | success-shutdown | ignore

        # (Optional) Defines what happens when each of the named health checks
        # fail. Possible values are:
        #
        # - restart: restart the service once
        # - shutdown: shut down and exit the Pebble daemon (with exit code 11)
        # - success-shutdown: shut down and exit Pebble with exit code 0
        # - ignore: do nothing further
        on-check-failure:
            <check name>: restart | shutdown | success-shutdown | ignore

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
        # For the health endpoint, ready implies alive, and not-alive implies
        # not-ready (but not the other way around). See the "Health endpoint"
        # section in the docs for details.
        level: alive | ready

        # (Optional) Control whether the check is started automatically when
        # Pebble starts or performs a 'replan' operation. Default is "enabled".
        startup: enabled | disabled

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
        # specified URL returns a 2xx status code.
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
