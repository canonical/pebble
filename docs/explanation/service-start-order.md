# Service start order

When multiple services need to be started together, they're started in order according to the `before` and `after` in the [layer configuration](../reference/layer-specification.md). Pebble waits 1 second after starting each service to ensure the command doesn't exit too quickly.

The `before` option is a list of services that this service must start before (it may or may not `requires` them, see [Service dependencies](./service-dependencies.md)). Or if it's easier to specify this ordering the other way around, `after` is a list of services that this service must start after.

```{include} /reuse/service-start-order.md
   :start-after: Start: Service start order note
   :end-before: End: Service start order note
```

## Service auto-restart

By default, if a service exits, Pebble will restart it after the backoff delay, which defaults to half a second. For more information, see [start command](/reference/cli-commands/start.md).
