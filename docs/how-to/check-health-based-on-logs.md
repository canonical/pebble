# How to check health based on logs

Pebble stores the most recent `stdout` and `stderr` from each service. This guide demonstrates how to set up a check that fails if the logs contain a particular line, such as a warning message.

This method is an alternative to directly querying a service. Directly querying a service is typically a more reliable method of catching issues. For more information, see [](./run-services-reliably).

## Define a layer

We'll define service called `foo` and a check called `foo-warning` that inspects the logs from `foo`:

```yaml
services:
  foo:
    override: replace
    command: /bin/foo
    startup: enabled

checks:
  foo-warning:
    override: replace
    threshold: 1
    exec:
      command: bash -c '! pebble logs | grep -q "\\[foo\\] .*WARNING"'
      # Because of YAML escaping rules, we need to use \\[ to pass \[ to grep.
```

The check searches Pebble's service logs for lines matching the pattern, such as:

```text
2025-04-26T03:22:20.315Z [foo] some WARNING reported by the service
```

If a match is found, the `bash -c '...'` command exits with 1 and the check fails.

By default, Pebble keeps a service running even if a check fails. Although we can configure a service to restart on check failure, this approach isn't helpful here. That's because restarting `foo` won't remove the warning from the logs, so the check remains "down", and subsequent failures won't trigger additional restarts. In this particular case, it's better to monitor the check and alert a human operator to investigate.

## Get the status of the check

To display the status of the check:

```{terminal}
   :input: pebble health foo-warning
unhealthy
```

This command exits with 0 if the check is healthy, or 1 if the check is unhealthy.

To display detailed status information about the check:

```{terminal}
   :input: pebble check foo-warning
name: foo-warning
startup: enabled
status: down
successes: 0
failures: 3
threshold: 1
change-id: "60"
logs: |
    2025-04-26T11:22:27+08:00 ERROR exit status 1
    2025-04-26T11:22:37+08:00 ERROR exit status 1
    2025-04-26T11:22:47+08:00 ERROR exit status 1
```

## See more

- [`pebble check`](#reference_pebble_check_command) command
- [`pebble health`](#reference_pebble_health_command) command
- [`pebble logs`](#reference_pebble_logs_command) command
- [](/reference/health-checks)
