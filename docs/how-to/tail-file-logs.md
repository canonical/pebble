# How to tail file logs

Some applications do not log to `stdout` or `stderr`, but instead write logs directly to
files such as `access.log`, `audit.log` or `error.log`. While Pebble does not natively
support tailing files, you can use `tail` from `coreutils` in a separate Pebble service
to route file contents into Pebble’s log stream.

This guide shows how to set that up.

## Example Pebble layer

Here’s how to tail a log file alongside your main service:

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

The separate helper service `tail -F` starts before the main service.
The `-F` option ensures that logs are captured reliably, even if log files are rotated.

This setup handles a variety of common corner cases:
- the log file doesn't exist when the service starts
- the log file is truncated, deleted or replaced
- a safety net if the log file is large at startup
- controlled stop order if services are disabled

A separate helper service is required for each log file.

## Rockcraft

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

## Further reading

This recipe can be composed with [forwarding logs to Loki](./forward-logs-to-loki.md).

Consider Promtail instead of `tail` for advanced use cases or if the latter proves to be
insufficient.
