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

## Include `tail` in a chiselled rock

Chiselled rocks often exclude tools like `coreutils`. To add `/usr/bin/tail`, extend
[the official example](https://documentation.ubuntu.com/rockcraft/en/latest/how-to/rocks/chisel-existing-rock/)
by staging the `coreutils_bins` slice in your `rockcraft.yaml` file:

```yaml
  install-python-slices:
    plugin: nil
    stage-packages:
      - base-files_release-info
      - python3.11_core
      - coreutils_bins  # includes /usr/bin/tail and the required libraries
```

The `coreutils_bins` slice brings in around a dozen shared objects and some hundred binaries.
For a truly minimal rock, consider
[defining a custom chisel slice](https://documentation.ubuntu.com/rockcraft/en/latest/how-to/chisel/create-slice/).

See the
[Rockcraft documentation](https://documentation.ubuntu.com/rockcraft/en/stable/)
to learn more.

## See more

- After capturing logs, you can [forward the logs to Loki](./forward-logs-to-loki).
- If `tail` is insufficient, consider using the
  [Promtail binary](https://github.com/canonical/loki-k8s-operator/blob/main/.github/workflows/build-promtail-release.yaml)
  instead.
