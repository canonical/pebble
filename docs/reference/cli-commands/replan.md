(reference_pebble_replan_command)=
# replan command

The replan command starts, stops, or restarts services that have changed, so that running services exactly match the desired configuration in the current plan.

## Usage

<!-- START AUTOMATED OUTPUT -->
```{terminal}
:input: pebble replan --help
Usage:
  pebble replan [replan-OPTIONS]

The replan command starts, stops, or restarts services that have changed,
so that running services exactly match the desired configuration in the
current plan.

[replan command options]
      --no-wait    Do not wait for the operation to finish but just print the
                   change id.
```
<!-- END AUTOMATED OUTPUT -->

## How it works

When you update service configuration (by adding a layer), the services changed won't be automatically restarted. `pebble replan ` restarts them and brings the service state in sync with the new configuration.

- The "replan" operation restarts `startup: enabled` services whose configuration have changed between when they started and now; if the configuration hasn't changed, replan does nothing.
- Replan also starts `startup: enabled` services that have not yet been started.

## Examples

Here is an example, where `srv1` is a service that has `startup: enabled`, and `srv2` does not:

```{terminal}
   :input: pebble replan
2023-04-25T15:06:50+02:00 INFO Service "srv1" already started.
```

Update "srv1" config:

```{terminal}
   :input: pebble add lay1 layer.yaml
Layer "lay1" added successfully from "layer.yaml"
```

Replan:

```{terminal}
   :input: pebble replan
Stop service "srv1"
Start service "srv1"
```

Change "srv2" to "startup: enabled"

```{terminal}
   :input: pebble add lay2 layer.yaml
Layer "lay2" added successfully from "layer.yaml"
```

Replan again:

```{terminal}
   :input: pebble replan
2023-04-25T15:11:22+02:00 INFO Service "srv1" already started.
Start service "srv2"
```

```{note}
If you want to force a service to restart even if its service configuration hasn't changed, use `pebble restart <service>`.
```