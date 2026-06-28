(security)=

# Security


## Product architecture

Pebble runs as a long-lived daemon that supervises a set of services on behalf of an administrator. Its trust boundaries follow from that position: clients reach the daemon over a Unix socket (with an optional limited TCP surface), the daemon manages services as configured, and persistent state lives in the `$PEBBLE` directory on disk.

```{mermaid}
flowchart LR
    Client["Pebble client or<br/>third-party client"]
    HTTP["Open access to<br/>limited endpoints"]
    Daemon["Daemon<br/>(service / exec /<br/>check / log managers)"]
    subgraph State["$PEBBLE directory"]
        StateDB[("State DB<br/>+ layer files<br/>+ Unix socket")]
    end
    HostFS["Service files, exec targets,<br/>working dirs, check files"]
    Services["Managed services"]
    Logs["Log targets<br/>(such as Loki)"]

    Client -- "HTTP over socket<br/>(local UID or basic auth)" --> Daemon
    HTTP -- "HTTP over TCP<br/>(unauthenticated)" --> Daemon
    Daemon -- "fork/exec, signals,<br/>service-log capture" --> Services
    Daemon -- "read / write state" --> StateDB
    Daemon -- "read / write<br/>(as configured)" --> HostFS
    Daemon -- "forward service logs" --> Logs
```

More detail:

Clients reach the daemon over a Unix socket in the `$PEBBLE` directory. The daemon authorises each request from the connecting user's UID, mapping it to a Pebble identity; the `basic` identity type uses a hashed password sent over the same socket.

When the daemon is started with `--http`, a deliberately limited set of open-access endpoints is exposed over TCP without authentication.

The daemon runs with the privileges it was started with and launches services as configured, so a service is only as isolated from the daemon as the host makes it. Beyond `$PEBBLE`, the daemon can read or write any path that its configured services, exec commands, and checks touch — bounded by the privileges it was started with.

The daemon's persistent state lives in the `$PEBBLE` directory — the state DB, the Unix socket, and any layer files — which defaults to `/var/lib/pebble/default`. Its confidentiality and integrity depend on the directory's permissions. Setting `PEBBLE_PERSIST=never` keeps the state in memory only, so the on-disk footprint reduces to the layer files and Unix socket. The directory can be seeded on first start from another location with `PEBBLE_COPY_ONCE`. The Pebble CLI stores its own client-side data files under `$XDG_CONFIG_HOME` (`$HOME/.config` by default), separate from the daemon's `$PEBBLE` directory.

## Secure by design

Pebble keeps its security surface small by default.

The API is served over a Unix socket rather than a network port, so out of the box the daemon is reachable only by local users and access decisions are made from the kernel-supplied UID rather than from credentials sent over the wire. Exposing endpoints over the network is opt-in through the `--http` option, and the endpoints made available that way are deliberately limited to a read-only, open-access set. No authentication is required to connect to the open-access endpoints.

Finer-grained access is granted explicitly through UID-keyed identities rather than being on by default. The daemon's persistent footprint is bounded to the `$PEBBLE` directory, which keeps the state that needs protecting in one well-defined location.

For more information, see [](api-and-clients.md) and [](../how-to/manage-identities.md).


(the-pebble-directory)=
## The Pebble directory

By default, Pebble stores its configuration, internal state, and Unix socket in the directory specified by the `PEBBLE` environment variable. If `$PEBBLE` is not set, Pebble uses the directory `/var/lib/pebble/default`.

The `$PEBBLE` directory must be readable and writable by the UID of the pebble process. Make sure that no other UIDs can read or write to the `$PEBBLE` directory. You can do that with `chmod`, for example:

```{terminal}
chmod 700 /var/lib/pebble/default
```

The file `$PEBBLE/.pebble.state` contains the internal state of the Pebble daemon. You shouldn't try to edit this file or change its permissions.

If `$PEBBLE_PERSIST` is set to `never`, then Pebble will only keep the state in memory without persisting it to the state file.


## Hardening

To harden a Pebble deployment:

- Use the default Unix-socket API rather than the `--http` option. The open-access endpoints exposed by `--http` require no authentication, so you should only enable them when you genuinely need unauthenticated network access to that limited endpoint set.
- Give each client the least-privileged Pebble identity it needs rather than sharing the admin identity. For more information, see [](../how-to/manage-identities.md).
- Apply network isolation so that any exposed ports are reachable only from the hosts and networks that need them, using host firewall rules or network policies as appropriate to your environment.
- Restrict the `$PEBBLE` directory permissions so it's only accessible to the Pebble process UID. See [](#the-pebble-directory).

FIPS 140-compliant builds of Pebble are available as a separate distribution channel. See [Cryptographic technology](#cryptographic-technology).


## Security lifecycle

Pebble is released as the [`pebble` snap](https://snapcraft.io/pebble) and as source releases on GitHub. Pebble follows [semantic versioning](https://semver.org/): major version for incompatible API changes, minor version for backwards-compatible feature additions, and patch version for backwards-compatible bug fixes.

The easiest way to ensure that you get security updates is to install the snap; see [](#install_pebble_snap). The snap's `latest` channel provides the current release and the `fips` channel provides the FIPS-compliant builds. Updates are delivered automatically by snap refresh; refer to the [snap documentation](https://snapcraft.io/docs/managing-updates) to schedule or defer refreshes, bearing in mind that delaying a refresh delays security fixes.

The versions of Pebble under security maintenance are listed in [SECURITY.md](https://github.com/canonical/pebble/blob/master/SECURITY.md): security updates are released for major versions that have had releases in the last year and for the versions of Pebble bundled with [maintained Juju releases](https://canonical.com/juju/docs/ops/latest/explanation/versions/). Long Term Support (LTS) releases receive five years of standard support, with up to ten additional years of [extended support](https://ubuntu.com/security/esm).

To verify the installed version of the daemon, run `pebble version`.


(cryptographic-technology)=
## Cryptographic technology

Pebble uses cryptography in a small number of well-defined places: hashing passwords for the basic identity type, and TLS for the optional HTTPS API. FIPS 140-compliant builds are distributed separately.

### Basic identity type

For the "basic" [identity](/reference/identities) type, Pebble uses Ulrich Drepper's SHA-crypt algorithm with SHA-512 (a 512-bit digest). Specifically, we use the third party Go library [github.com/GehirnInc/crypt](https://github.com/GehirnInc/crypt) for verifying the password hashes sent in a client's `Authorization` HTTP header.

### TLS

Pebble uses the TLS code in Go's standard library when the `--https` argument is passed to `pebble run`, enabling API access over TLS. Pebble negotiates TLS 1.2 or TLS 1.3; older protocol versions are not offered.

Server-side TLS certificates are managed by Pebble. On first start, a Pebble identity certificate is generated using Ed25519 (256-bit keys). Incoming HTTPS requests use ephemeral TLS certificates, self-signed with the identity certificate. There is no support for integration with an external certificate authority.

Currently, the Pebble client doesn't support HTTPS (TLS). To connect to a Pebble daemon over HTTPS, you'll need to make [API](/reference/api) calls using `curl --insecure`, for example.

Our intention is that projects that build on Pebble can [override how TLS connections are verified](https://pkg.go.dev/github.com/canonical/pebble@v1.23.0/client#Config).

(fips-140)=
### FIPS 140

[FIPS 140](https://en.wikipedia.org/wiki/FIPS_140)-compliant builds of Pebble are available: the source code is in the [`fips` branch](https://github.com/canonical/pebble/tree/fips) and the [`pebble` snap](https://snapcraft.io/pebble) has a `fips` track. These builds take the conservative approach of removing access to cryptographic primitives entirely, pending FIPS validation of the Go runtime:

* Basic-auth password hashing is removed.
* The third-party SHA-crypt library is removed, so only pre-hashed password identities can authenticate.
* The `--https` flag is unavailable so the daemon will not serve HTTPS.
* Outbound requests from the CLI, checks, and log targets are restricted to HTTP (HTTPS origins and HTTP-to-HTTPS redirects fail).

See [HACKING.md](https://github.com/canonical/pebble/blob/fips/HACKING.md#fips-140-changes) in the `fips` branch for the authoritative list of changes.

### Cryptographic packages

The cryptographic functionality Pebble uses is provided by two sources. The first is the Go standard library, which supplies:

* TLS and certificate handling (`crypto/tls`, `crypto/x509`).
* Key generation and signing for the identity certificate (`crypto/ed25519`, `crypto/rand`).
* SHA-512 hashing (`crypto/sha512`).

The second source is the third-party [github.com/GehirnInc/crypt](https://github.com/GehirnInc/crypt) library, which provides the SHA-crypt password hashing used for the basic identity type.

The [FIPS builds](#fips-140) remove these cryptographic primitives rather than replacing them.

### Encryption at rest

Pebble does not encrypt state at rest. Confidentiality at rest relies on the `$PEBBLE` directory permissions and on the host's at-rest encryption story. When `$PEBBLE_PERSIST=never` is set, state is held only in memory and the persistence concern does not apply.


## Logging and monitoring

### Daemon logs

The Pebble daemon writes its own log messages to its standard error stream as one line per message. Each line carries an ISO-8601 timestamp, a level marker, and a free-text message. For example, `2026-06-24T08:00:00.000Z [pebble] HTTP API server listening on ":4000".`.

Two levels are emitted: `Notice` is always on and is intended to be user-visible; `Debug` is gated on the `PEBBLE_DEBUG=1` environment variable.

The host's service manager or container runtime collects the stream — there is no separate log file or rotation managed by Pebble itself.

### Service logs

The standard output and standard error of the services Pebble manages are captured and made available, including as a stream, through the logs API and the [`pebble logs`](#reference_pebble_logs_command) command. Passing `--verbose` (or setting `PEBBLE_VERBOSE=1`) on `pebble run` additionally mirrors service output onto the daemon's own standard output. Service logs can be forwarded to external collectors (for example, Loki) through the log-target configuration in the layer plan.

### Notices

Pebble also records notices, such as warnings and change updates, which clients can list and wait on through the notices API. See [](/reference/notices).

### Security events

Security events are interleaved with the free-text daemon log lines. To collect security events, filter on `"type":"security"`.

Each event is a single JSON object on its own line:

```json
{
  "type": "security",
  "datetime": "2026-06-24T08:00:00.000Z",
  "level": "WARN",
  "event": "authz_fail:42,/v1/services",
  "description": "UID 42 denied access to /v1/services",
  "appid": "pebble"
}
```

`datetime` is RFC 3339 UTC, `level` is `WARN` or `CRITICAL`, `event` is the OWASP event name optionally suffixed with `:<arg>`, and `appid` defaults to `pebble`.

Security events follow the [OWASP application logging vocabulary](https://cheatsheetseries.owasp.org/cheatsheets/Logging_Vocabulary_Cheat_Sheet.html). Pebble emits these events:

| Event | Emitted when |
| --- | --- |
| `sys_startup` | The daemon starts. |
| `sys_shutdown` | The daemon shuts down. |
| `sys_monitor_disabled` | Service health monitoring is disabled. |
| `authz_admin` | An admin-level authorisation decision is made. |
| `authz_fail` | An authorisation check fails, including TLS client-certificate verification failures. |
| `user_created` | An identity is added to the identities database. |
| `user_updated` | An identity in the database is modified. |
| `user_deleted` | An identity is removed from the database. |


## Decommissioning

To remove Pebble, uninstall it using the method it was installed with, such as `sudo snap remove pebble` for the snap. If you no longer need the daemon's state, also remove the `$PEBBLE` directory (`/var/lib/pebble/default` by default). This directory can contain identity secrets and service state, so treat its removal as you would any other deletion of sensitive data.

On a FIPS deployment, remove the FIPS-build snap and its `$PEBBLE` directory in the same way.


## Reporting vulnerabilities

If you believe you have found a security vulnerability in Pebble, please report it privately following the instructions in the [SECURITY.md](https://github.com/canonical/pebble/blob/master/SECURITY.md) file in the project repository.

Reports are handled according to the [Ubuntu Security disclosure and embargo policy](https://ubuntu.com/security/disclosure-policy), which describes how researchers, users, and customers can responsibly disclose issues to Canonical.

Information about known vulnerabilities affecting Pebble is published in:

* The [GitHub Security Advisories for `canonical/pebble`](https://github.com/canonical/pebble/security/advisories).
* The [Pebble release notes](https://github.com/canonical/pebble/releases) on GitHub.
* Relevant [Ubuntu Security Notices](https://ubuntu.com/security/notices) when a vulnerability also affects an Ubuntu-packaged copy of Pebble.
