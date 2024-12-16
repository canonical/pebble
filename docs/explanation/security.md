# Security


## Access to the API

The Pebble daemon exposes an API that enables remote clients to interact with the daemon. The API uses HTTP over a Unix socket, with access to the API controlled by user ID (UID). If you want to grant a specific access level to a user, you can define an "identity" for the user.

If you use the `--http` option when starting the daemon, Pebble exposes a limited set of open-access API endpoints over TCP. No authentication is required to connect to the open-access endpoints.

For more information, see [](api-and-clients.md) and [](../how-to/manage-identities.md).


## The Pebble directory

Pebble stores its configuration, internal state, and Unix socket in the directory specified by the `PEBBLE` environment variable. If `$PEBBLE` is not set, Pebble uses the directory `/var/lib/pebble/default`.

The `$PEBBLE` directory must be readable and writable by the UID of the pebble process. Make sure that no other UIDs can read or write to the $PEBBLE directory.

The file `$PEBBLE/.pebble.state` contains the internal state of the Pebble daemon. You shouldn't try to edit this file or change its permissions.


## Security updates

There are several ways to install Pebble. The easiest way to ensure that you get security updates is to [install the snap](#install_pebble_snap).
