# How to check health based on logs

<!-- TODO: Need to be clear that this is not the primary way to check health. It's kind of a hack. This is an aid to monitoring/diagnosis. It's not recommended as an unattended way to ensure that a service runs reliably. -->

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
```

The check searches Pebble's service logs for lines such as:

```text
2025-04-26T03:22:20.315Z [foo] some WARNING reported by the service
```

If a match is found, the `bash -c '...'` command exits with 1 and the check fails.

When the check fails, Pebble doesn't restart `foo`. To learn how to automatically restart `foo`, see [](#restart-a-service-when-the-health-check-fails). However, we recommend that you don't configure `foo` to restart. After restarting `foo`, Pebble's service logs would still contain the warning line, so the check would still be considered "down" and Pebble wouldn't restart `foo` if another warning is logged.

Instead of configuring `foo` to restart, we recommend that you monitor the check and alert a human operator if the check fails.

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
failures: 3
threshold: 1
change-id: "60"
logs: |
    2025-04-26T11:22:27+08:00 ERROR exit status 1
    2025-04-26T11:22:37+08:00 ERROR exit status 1
```

## See more

- [`pebble health`](#reference_pebble_health_command) command
- [`pebble check`](#reference_pebble_check_command) command
- [](/reference/health-checks)
