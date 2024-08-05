(reference_pebble_stop_command)=
# stop command

The stop command stops the service with the provided name and any other service that depends on it, in the correct order.

## Usage

<!-- START AUTOMATED OUTPUT -->
```{terminal}
:input: pebble stop --help
Usage:
  pebble stop [stop-OPTIONS] <service>...

The stop command stops the service with the provided name and
any other service that depends on it, in the correct order.

[stop command options]
      --no-wait      Do not wait for the operation to finish but just print the
                     change id.
```
<!-- END AUTOMATED OUTPUT -->

## How it works

When stopping a service, Pebble sends SIGTERM to the service's process group, and waits up to 5 seconds. If the command hasn't exited within that time window, Pebble sends SIGKILL to the service's process group and waits up to 5 more seconds. If the command exits within that 10-second time window, the stop is considered successful, otherwise `pebble stop` will exit with an error, regardless of the `on-failure` value.

## Examples

To stop specific services, use `pebble stop` followed by one or more service names. The following example stops one service named "srv1":

```bash
pebble stop srv1
```
