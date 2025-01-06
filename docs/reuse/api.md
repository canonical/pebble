Start: Pebble API overview

Pebble exposes an HTTP API that remote clients can use to interact with the daemon. The API has endpoints for starting and stopping services, adding configuration layers to the plan, and so on.

The API uses HTTP over a Unix socket, with access to the API controlled by user ID. If `pebble run` is started with the `--http <address>` option, Pebble exposes a limited set of open-access endpoints (see {ref}`api-access-levels`) using the given TCP address.

End: Pebble API overview
