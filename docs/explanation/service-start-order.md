# Service start order

When multiple services need to be started together, they're started in order according to the `before` and `after` in the [layer configuration](../reference/layer-specification.md). Pebble [waits 1 second](#reference_pebble_start_command) after starting each service to ensure the command doesn't exit too quickly.

The `before` option is a list of services that this service must start before (it may or may not `requires` them, see [Service dependencies](./service-dependencies.md)). Or if it's easier to specify this ordering the other way around, `after` is a list of services that this service must start after.

```{include} /reuse/service-start-order.md
   :start-after: Start: Service start order note
   :end-before: End: Service start order note
```

The `before` and `after` options are not designed for scenarios where you need to start service B only after service A has exited. A common workaround is to combine both services into a single service definition, using a command such as `bash -c 'run-service-a && run-service-b'` to ensure that service B starts only after service A exits successfully.

For comparison, in systemd this can be achieved by running service B from service A's `ExecStopPost=` directive. In supervisord, this can be achieved using event listeners.
