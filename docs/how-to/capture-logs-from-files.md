# How to capture logs from files

Some applications do not log to `stdout` or `stderr`, but instead write logs directly to
files such as `access.log`, `audit.log` or `error.log`. While Pebble does not natively
support tailing files, you can use `tail` from `coreutils` in a separate Pebble service
to route file contents into Pebble's log stream.

This guide shows how to set that up.

## Define a layer

Here's how to tail a log file alongside your main service:

```{code-block} yaml
  :emphasize-lines: 5, 7, 10

services:
  foo:
    command: foo
    startup: enabled
    requires:
      - foo-error-log
    after:
      - foo-error-log
  foo-error-log:
    command: tail -F /var/log/foo/error.log
    startup: enabled
```

The helper service `foo-error-log` starts before the main service.
The `-F` option of the `tail` command ensures that logs are captured reliably,
even if log files are rotated.

This setup handles common corner cases:
- The log file doesn't exist when the service starts
- The log file is truncated, deleted or replaced
- A safety net if the log file is large at startup
- Controlled stop order if services are disabled

A separate helper service is required for each log file.

## Include `tail` in a rock image

If your workload is packaged as a rock, you may need to explicitly add `tail`:

```yaml
parts:
  tail:
    plugin: nil
    stage-packages:
      - coreutils
    organize:
      usr/bin/tail: usr/bin/tail
```

## See more

- After capturing logs, you can [forward the logs to Loki](./forward-logs-to-loki).
- If `tail` is insufficient, consider using Promtail instead of `tail`.
