# How to capture logs from files

Pebble stores the most recent `stdout` and `stderr` from each service. However, some
applications don't log to `stdout` or `stderr`. Instead, they write logs directly to
files such as `access.log`, `audit.log`, or `error.log`.

This guide demonstrates how to use the `tail` command to print log files to `stdout`,
so that Pebble can capture the logs. The `tail` command is provided by the `coreutils`
package.

## Define a layer

To use `tail`, we'll define a layer that has a main service (`foo`) and a helper service
for `tail`:

```{code-block} yaml
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
Likewise, if the services are disabled, the helper service `foo-error-log` will be
stopped after the main service `foo`.
This ensures that logs emitted during both startup and termination are captured.

The `-F` option of the `tail` command ensures that logs are captured reliably,
even if log files are rotated.

This setup handles common corner cases:
- The log file doesn't exist when the service starts
- The log file is truncated, deleted or replaced
- A safety net if the log file is large at startup

A separate helper service is required for each log file.

## Include `tail` in a rock image

If your main service runs inside a container image created by Rockcraft,
you might need to explicitly include `tail` when you build the image.

To include `tail`, add the following part to your `rockcraft.yaml` file:

```yaml
parts:
  tail:
    plugin: nil
    stage-packages:
      - coreutils
    organize:
      usr/bin/tail: usr/bin/tail
```

To learn more about Rockcraft and `rockcraft.yaml` files, see the
[Rockcraft documentation](https://documentation.ubuntu.com/rockcraft/en/stable/).

## See more

- After capturing logs, you can [forward the logs to Loki](./forward-logs-to-loki).
- If `tail` is insufficient, consider using the Promtail binary instead.
