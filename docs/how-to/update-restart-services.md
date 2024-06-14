# How to update and restart services

When you update service configuration (by adding a layer), the services changed won't be automatically restarted. To restart them and bring the service state in sync with the new configuration, use `pebble replan`.

The "replan" operation restarts `startup: enabled` services whose configuration have changed between when they started and now; if the configuration hasn't changed, replan does nothing. Replan also starts `startup: enabled` services that have not yet been started.

Here is an example, where `srv1` is a service that has `startup: enabled`, and `srv2` does not:

```
$ pebble replan
2023-04-25T15:06:50+02:00 INFO Service "srv1" already started.
$ pebble add lay1 layer.yaml  # update srv1 config
Layer "lay1" added successfully from "layer.yaml"
$ pebble replan
Stop service "srv1"
Start service "srv1"
$ pebble add lay2 layer.yaml  # change srv2 to "startup: enabled"
Layer "lay2" added successfully from "layer.yaml"
$ pebble replan
2023-04-25T15:11:22+02:00 INFO Service "srv1" already started.
Start service "srv2"
```

If you want to force a service to restart even if its service configuration hasn't changed, use `pebble restart <service>`.
