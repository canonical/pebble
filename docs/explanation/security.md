(security)=

# Security


## Product architecture

Pebble runs as a long-lived daemon that supervises a set of services on behalf of an administrator. Its security posture is shaped by three trust boundaries. The first is between the users who interact with the daemon and the daemon itself: clients connect over a Unix socket and the daemon authorises each request from the connecting user's UID, mapping it to a Pebble identity, as described in the Access to the API section below. The second is between the daemon and the services it manages: the daemon runs with the privileges it was started with and launches services as configured, so a service is only as isolated from the daemon as the host makes it. The third is between the daemon and its on-disk state in the `$PEBBLE` directory, whose confidentiality and integrity depend on the directory permissions described in The Pebble directory section below. When the daemon is started with `--http`, a further, untrusted boundary is exposed: a limited set of open-access endpoints reachable over TCP without authentication.


## Secure by Design

Pebble is designed to keep its security surface small by default. The API is served over a Unix socket rather than a network port, so out of the box the daemon is reachable only by local users and access decisions are made from the kernel-supplied UID rather than from credentials sent over the wire. Exposing endpoints over the network is opt-in through the `--http` option, and the endpoints made available that way are deliberately limited to a read-only, open-access set. Finer-grained access is granted explicitly through UID-keyed identities rather than being on by default. The daemon's persistent footprint is bounded to the `$PEBBLE` directory, which keeps the state that needs protecting in one well-defined location.


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

## Hardening

Beyond restricting the `$PEBBLE` directory permissions described above, you can further harden a Pebble deployment:

- Run the Pebble daemon as a non-root user wherever the managed workload permits it, so that a compromise of the daemon does not automatically confer root on the host.
- Prefer the default Unix-socket API over the `--http` option. The open-access endpoints exposed by `--http` require no authentication, so only enable them when you genuinely need unauthenticated network access to that limited endpoint set.
- When network access to the full API is required, use TLS by passing `--https` rather than `--http`, so that traffic is encrypted in transit. See the TLS sub-section below.
- Give each client the least-privileged Pebble identity it needs rather than sharing the admin identity. For more information, see [](../how-to/manage-identities.md).
- Apply network isolation so that any exposed ports are reachable only from the hosts and networks that need them, using host firewalling or network policies as appropriate to your environment.

## Security updates

There are several ways to install Pebble. The easiest way to ensure that you get security updates is to [install the snap](#install_pebble_snap).

### Security lifecycle

The versions of Pebble under security maintenance are listed in [SECURITY.md](https://github.com/canonical/pebble/blob/master/SECURITY.md): security updates are released for major versions that have had releases in the last year and for the versions of Pebble bundled with maintained Juju releases. The recommended way to receive these updates is the [`pebble` snap](https://snapcraft.io/pebble); track its `latest` channel to stay on the current release, and track the `fips` channel for updates to the FIPS-compliant builds. Updates are delivered automatically by snap refresh; refer to the [snap documentation](https://snapcraft.io/docs/managing-updates) to schedule or defer refreshes, bearing in mind that delaying a refresh delays security fixes. To verify the installed version, run `pebble version`. There is no separate long-term-support track: a version leaves security maintenance once it falls outside the window described in SECURITY.md.


## Cryptographic technology

Pebble uses cryptography in a small number of well-defined places: hashing passwords for the basic identity type, TLS for the optional HTTPS API, and the FIPS 140-compliant builds. The sub-sections below describe each of these, the packages that provide the cryptographic functionality, and Pebble's approach to data at rest.

### Basic identity type

For the "basic" [identity](/reference/identities) type, Pebble uses Ulrich Drepper's SHA-crypt algorithm with SHA-512. Specifically, we use the third party Go library [github.com/GehirnInc/crypt](https://github.com/Gehirninc/crypt) for verifying the password hashes sent in a client's `Authorization` HTTP header.

### TLS

Pebble uses the TLS code in Go's standard library when the `--https` argument is passed to `pebble run`, enabling API access over TLS.

Server-side TLS certificates are managed by Pebble. On first start, a Pebble identity certificate is generated. Incoming HTTPS requests will use ephemeral TLS certificates, self-signed with the identity certificate. There is currently no support for integration with an external certificate authority.

Currently, the Pebble client doesn't support HTTPS (TLS). To connect to a Pebble daemon over HTTPS, you'll need to make [API](/reference/api) calls using `curl --insecure`, for example.

Our intention is that projects that build on Pebble can [override how TLS connections are verified](https://pkg.go.dev/github.com/canonical/pebble@v1.23.0/client#Config).

### FIPS 140

This project also distributes [FIPS 140](https://en.wikipedia.org/wiki/FIPS_140)-compliant builds of Pebble: the source code is in the `fips` branch and there's the `fips` track for the official [`pebble` snap](https://snapcraft.io/pebble). Refer to [HACKING.md](https://github.com/canonical/pebble/blob/fips/HACKING.md#fips-140-changes) in the `fips` branch for the list of limitations in the FIPS builds.

### Cryptographic packages

The cryptographic functionality Pebble uses is provided by two sources. The first is the Go standard library, which supplies TLS and certificate handling (`crypto/tls`, `crypto/x509`), key generation and signing for the identity certificate (`crypto/ed25519`, `crypto/rand`), and SHA-512 hashing (`crypto/sha512`). The second is the third-party [github.com/GehirnInc/crypt](https://github.com/GehirnInc/crypt) library, which provides the SHA-crypt password hashing used for the basic identity type. The FIPS builds replace the standard library's cryptographic primitives with a FIPS-validated module, as described in the FIPS 140 sub-section above.

### Encryption at rest

Pebble does not encrypt state at rest. Confidentiality at rest relies on the `$PEBBLE` directory permissions described in The Pebble directory section above and on the host's at-rest encryption story. When `$PEBBLE_PERSIST=never` is set, state is held only in memory and the persistence concern does not apply.


## Logging and monitoring

The Pebble daemon writes structured logs to its standard error stream, which the host's service manager or container runtime can collect. The standard output and standard error of the services Pebble manages are captured and made available, including as a stream, through the logs API and the [`pebble logs`](#reference_pebble_logs_command) command. Pebble also records notices, such as warnings and change updates, which clients can list and wait on through the notices API; for more information, see [](/reference/notices).

OWASP-vocabulary security event logging is being introduced in [canonical/pebble#874](https://github.com/canonical/pebble/pull/874); this section will be expanded with the event vocabulary when that PR lands.


## Secure decommissioning

To remove Pebble, uninstall it using the method it was installed with, such as `sudo snap remove pebble` for the snap. If you no longer need the daemon's state, also remove the `$PEBBLE` directory (`/var/lib/pebble/default` by default). This directory can contain identity secrets and service state, so treat its removal as you would any other deletion of sensitive data. On a FIPS deployment, remove the FIPS-build snap and its `$PEBBLE` directory in the same way.


## Reporting vulnerabilities

Security vulnerabilities in Pebble should be reported privately. See [SECURITY.md](https://github.com/canonical/pebble/blob/master/SECURITY.md) for the disclosure process and the supported-version policy. Known vulnerabilities and their fixes are published as [GitHub Security Advisories](https://github.com/canonical/pebble/security/advisories).
