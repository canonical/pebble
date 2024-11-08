# Service dependencies

Pebble can take service dependencies into account when managing services: this is done with the `requires` list in the [service definition](../reference/layer-specification.md).

Simply put, you can configure a list of other services in the `requires` section to indicate this service requires those other services to start correctly.

When Pebble starts a service, it also starts the services which that service depends on (configured with `requires`). Conversely, when stopping a service, Pebble also stops services which depend on that service.

For the start order of the services, see [Service start order](./service-start-order.md).

For example, if service `nginx` requires `logger`, `pebble start nginx` will start both `nginx` and `logger` (in an undefined order). Running `pebble stop logger` will stop both `nginx` and `logger`; however, running `pebble stop nginx` will only stop `nginx` (`nginx` depends on `logger`, not the other way around).
