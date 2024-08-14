# API and clients

The Pebble daemon exposes an API (HTTP over a unix socket) to allow remote clients to interact with the daemon. It can start and stop services, add configuration layers to the plan, and so on.

If `pebble run` is started with the `--http <address>` option, Pebble also allows access to "untrusted" HTTP endpoints using the given TCP address (see {ref}`api-access-levels` below).

There is currently no official documentation for the API at the HTTP level (apart from the [code itself](https://github.com/canonical/pebble/blob/master/internals/daemon/api.go)!); most users will interact with it via the Pebble command line interface or by using the Go or Python clients.

The Go client is used primarily by the CLI, but is importable and can be used by other tools too. See the [reference documentation and examples](https://pkg.go.dev/github.com/canonical/pebble/client) at pkg.go.dev.

We try to never change the underlying HTTP API in a backwards-incompatible way, however, in rare cases we may change the Go client in a backwards-incompatible way.

In addition to the Go client, there's also a [Python client](https://github.com/canonical/operator/blob/master/ops/pebble.py) for the Pebble API that's part of the [`ops` library](https://github.com/canonical/operator) used by Juju charms ([documentation here](https://juju.is/docs/sdk/interact-with-pebble)).


(api-access-levels)=
## API access levels

API endpoints fall into one of three access levels, from least restricted to most restricted:

* `untrusted`: these are allowed from any user, even unauthenticated users using the HTTP-over-TCP listener. The only untrusted endpoints are `/v1/system-info` and `/v1/health`.
* `read`: these are allowed from any authenticated user, regardless of access level. They are usually read operations that use the HTTP `GET` method, such as listing services or viewing notices.
* `admin`: these are only allowed from admin users. They are usually write or modify operations that use the HTTP `POST` method, such as adding a layer or starting a service.

Pebble authenticates clients that connect to the socket API using peer credentials ([`SO_PEERCRED`](https://man7.org/linux/man-pages/man7/socket.7.html)) to determine the user ID (UID) of the connecting process. If this UID is 0 (root) or the UID of the Pebble daemon, the user's access level is `admin`, otherwise the access level is `read`.

If Pebble can't authenticate the user at all, or if it's an unauthenticated user connecting over TCP, the user is considered `untrusted`.


## Controlling API access using identities

In addition to the default access control, Pebble admins can also set up named [identities](../reference/identities.md) with specific access levels using the identities CLI commands.

Each identity has a name, an access level, and an authentication type with type-specific configuration.

Currently the only authentication type is `local`, that is, a local user ID determined using peer credentials. An example admin identity named "bob" is shown below:

```yaml
identities:
    bob:
        access: admin
        local:
            user-id: 42
```

Read [how to manage identities](../how-to/manage-identities.md) for more information.
