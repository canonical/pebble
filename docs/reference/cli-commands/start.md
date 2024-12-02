(reference_pebble_start_command)=
# start command

The start command starts the service with the provided name and any other services it depends on, in the correct order.

## Usage

<!-- START AUTOMATED OUTPUT -->
```{terminal}
:input: pebble start --help
Usage:
  pebble start [start-OPTIONS] <service>...

The start command starts the service with the provided name and
any other services it depends on, in the correct order.

[start command options]
      --no-wait      Do not wait for the operation to finish but just print the
                     change id.
```
<!-- END AUTOMATED OUTPUT -->

## How it works

When starting a service, Pebble executes the service's `command`, and waits 1 second to ensure the command doesn't exit too quickly.

- If the command doesn't exit within the 1 second window, the start is considered successful.
- If the command exits within the 1 second window, Pebble retries the command after a backoff delay of half a second (default value). Within the 1 second window, Pebble applies the restart logic described in [Service auto-restart](/reference/service-auto-restart.md). You can configure the backoff delay and the auto-restart behaviour according to the service's exit code. For more information, see [Layer specification](/reference/layer-specification.md).

If the service exits after each start attempt within the 1 second window, pebble start exits with an error, regardless of the on-failure value.

## Examples

To start specific services, run `pebble start` followed by one or more service names. For example, to start two services named "srv1" and "srv2" (and any dependencies), run:

```bash
pebble start srv1 srv2
```
