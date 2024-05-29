# API and clients

The Pebble daemon exposes an API (HTTP over a unix socket) to allow remote clients to interact with the daemon. It can start and stop services, add configuration layers the plan, and so on.

There is currently no official documentation for the API at the HTTP level (apart from the [code itself](https://github.com/canonical/pebble/blob/master/internals/daemon/api.go)!); most users will interact with it via the Pebble command line interface or by using the Go or Python clients.

The Go client is used primarily by the CLI, but is importable and can be used by other tools too. See the [reference documentation and examples](https://pkg.go.dev/github.com/canonical/pebble/client) at pkg.go.dev.

We try to never change the underlying HTTP API in a backwards-incompatible way, however, in rare cases we may change the Go client in a backwards-incompatible way.

In addition to the Go client, there's also a [Python client](https://github.com/canonical/operator/blob/master/ops/pebble.py) for the Pebble API that's part of the [`ops` library](https://github.com/canonical/operator) used by Juju charms ([documentation here](https://juju.is/docs/sdk/interact-with-pebble)).
