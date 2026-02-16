(security)=

# Security


## Access to the API

The Pebble daemon exposes an API that enables remote clients to interact with the daemon. The API uses HTTP over a Unix socket, with access to the API controlled by user ID (UID). If you want to grant a specific access level to a user, you can define an "identity" for the user.

If you use the `--http` option when starting the daemon, Pebble exposes a limited set of open-access API endpoints over TCP. No authentication is required to connect to the open-access endpoints.

For more information, see [](api-and-clients.md) and [](../how-to/manage-identities.md).


## The Pebble directory

By default, Pebble stores its configuration, internal state, and Unix socket in the directory specified by the `PEBBLE` environment variable. If `$PEBBLE` is not set, Pebble uses the directory `/var/lib/pebble/default`.

The `$PEBBLE` directory must be readable and writable by the UID of the pebble process. Make sure that no other UIDs can read or write to the $PEBBLE directory. You can do that with `chmod`, for example:

```{terminal}
chmod 700 /var/lib/pebble/default
```

The file `$PEBBLE/.pebble.state` contains the internal state of the Pebble daemon. You shouldn't try to edit this file or change its permissions.

If `$PEBBLE_PERSIST` is set to "never", then Pebble will only keep the state in memory without persisting it to the state file.

## Security updates

There are several ways to install Pebble. The easiest way to ensure that you get security updates is to [install the snap](#install_pebble_snap).


## Cryptographic technology

### Basic identity type

For the "basic" [identity](/reference/identities) type, Pebble uses Ulrich Drepper's [SHA-crypt algorithm](https://www.akkadia.org/drepper/SHA-crypt.txt) with SHA-512. Specifically, we use the third party Go library [github.com/GehirnInc/crypt](https://github.com/Gehirninc/crypt) for verifying the password hashes sent in a client's `Authorization` HTTP header.

### TLS

Pebble uses the TLS code in Go's standard library when the `--https` argument is passed to `pebble run`, enabling API access over TLS.

Server-side TLS certificates are managed by Pebble. On first start, a Pebble identity certificate is generated. Incoming HTTPS requests will use ephemeral TLS certificates, self-signed with the identity certificate. There is currently no support for integration with an external certificate authority.

Currently, the Pebble client doesn't support HTTPS (TLS). To connect to a Pebble daemon over HTTPS, you'll need to make [API](/reference/api) calls using `curl --insecure`, for example.

Our intention is that projects that build on Pebble can [override how TLS connections are verified](https://pkg.go.dev/github.com/canonical/pebble@v1.23.0/client#Config).

### FIPS 140

This project also distributes [FIPS 140](https://en.wikipedia.org/wiki/FIPS_140)-compliant builds of Pebble: the source code is in the `fips` branch and there's the `fips` track for the official [`pebble` snap](https://snapcraft.io/pebble). Refer to [HACKING.md](https://github.com/canonical/pebble/blob/fips/HACKING.md#fips-140-changes) in the `fips` branch for the list of limitations in the FIPS builds.
