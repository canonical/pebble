(security)=

# Security


## Product architecture

Pebble runs as a long-lived daemon that supervises a set of services on behalf of an administrator. Its security posture is shaped by three trust boundaries.

```{mermaid}
flowchart LR
    Admin["Admin / operator<br/>(local UID)"]
    Client["Client / API user<br/>(local UID)"]
    HTTP["--http TCP client<br/>(unauthenticated)"]
    subgraph DaemonProc["Pebble daemon process"]
        Daemon["Daemon<br/>(service / exec /<br/>check / log managers)"]
    end
    subgraph State["$PEBBLE directory"]
        StateDB[("State DB<br/>+ layer files<br/>+ Unix socket")]
    end
    Services["Managed services"]
    Logs["Log targets<br/>(e.g. Loki)"]

    Admin -- "Unix socket<br/>(UID-keyed identity)" --> Daemon
    Client -- "Unix socket / --https TLS" --> Daemon
    HTTP -- "open-access endpoint set" --> Daemon
    Daemon -- "fork/exec, signals,<br/>service-log capture" --> Services
    Daemon -- "read / write state" --> StateDB
    Daemon -- "forward service logs" --> Logs
```

The first boundary is between users who interact with the daemon and the daemon itself. Clients connect over a Unix socket, and the daemon authorises each request from the connecting user's UID, mapping it to a Pebble identity. When the daemon is started with `--http`, a further untrusted boundary is exposed: a deliberately limited set of open-access endpoints reachable over TCP without authentication.

The second boundary is between the daemon and the services it manages. The daemon runs with the privileges it was started with and launches services as configured, so a service is only as isolated from the daemon as the host makes it.

The third boundary is between the daemon and its on-disk state in the `$PEBBLE` directory, whose confidentiality and integrity depend on directory permissions.

This section is drawn from Pebble's SSDLC threat model. The full asset and threat catalogue (crown jewels, stepping stones, and ranked threats per C/I/A) lives with the SSDLC artefacts; this page summarises the architecture-relevant boundaries.


## Secure by design

Pebble is designed to keep its security surface small by default.

The API is served over a Unix socket rather than a network port, so out of the box the daemon is reachable only by local users and access decisions are made from the kernel-supplied UID rather than from credentials sent over the wire. Exposing endpoints over the network is opt-in through the `--http` option, and the endpoints made available that way are deliberately limited to a read-only, open-access set.

Finer-grained access is granted explicitly through UID-keyed identities rather than being on by default. The daemon's persistent footprint is bounded to the `$PEBBLE` directory, which keeps the state that needs protecting in one well-defined location.


## Access to the API

The Pebble daemon exposes an API that enables remote clients to interact with the daemon. The API uses HTTP over a Unix socket, with access to the API controlled by user ID (UID). If you want to grant a specific access level to a user, you can define an "identity" for the user.

If you use the `--http` option when starting the daemon, Pebble exposes a limited set of open-access API endpoints over TCP. No authentication is required to connect to the open-access endpoints.

For more information, see [](api-and-clients.md) and [](../how-to/manage-identities.md).


## The Pebble directory

By default, Pebble stores its configuration, internal state, and Unix socket in the directory specified by the `PEBBLE` environment variable. If `$PEBBLE` is not set, Pebble uses the directory `/var/lib/pebble/default`.

The `$PEBBLE` directory must be readable and writable by the UID of the pebble process. Make sure that no other UIDs can read or write to the `$PEBBLE` directory. You can do that with `chmod`, for example:

```{terminal}
chmod 700 /var/lib/pebble/default
```

The file `$PEBBLE/.pebble.state` contains the internal state of the Pebble daemon. You shouldn't try to edit this file or change its permissions.

If `$PEBBLE_PERSIST` is set to `never`, then Pebble will only keep the state in memory without persisting it to the state file.


## Hardening

Beyond restricting the `$PEBBLE` directory permissions described above, you can further harden a Pebble deployment:

- Prefer the default Unix-socket API over the `--http` option. The open-access endpoints exposed by `--http` require no authentication, so only enable them when you genuinely need unauthenticated network access to that limited endpoint set.
- Give each client the least-privileged Pebble identity it needs rather than sharing the admin identity. For more information, see [](../how-to/manage-identities.md).
- Apply network isolation so that any exposed ports are reachable only from the hosts and networks that need them, using host firewalling or network policies as appropriate to your environment.

FIPS 140-compliant builds of Pebble are available as a separate distribution channel; see the [Cryptographic technology](#cryptographic-technology) section below.


## Security lifecycle

Pebble is released as the [`pebble` snap](https://snapcraft.io/pebble) and as source releases on GitHub. Pebble follows [semantic versioning](https://semver.org/): the major version changes for incompatible API changes, the minor version for backwards-compatible feature additions, and the patch version for backwards-compatible bug fixes.

The easiest way to ensure that you get security updates is to install the snap; see [](#install_pebble_snap). The snap's `latest` channel tracks the current release and the `fips` channel tracks the FIPS-compliant builds. Updates are delivered automatically by snap refresh; refer to the [snap documentation](https://snapcraft.io/docs/managing-updates) to schedule or defer refreshes, bearing in mind that delaying a refresh delays security fixes.

The versions of Pebble under security maintenance are listed in [SECURITY.md](https://github.com/canonical/pebble/blob/master/SECURITY.md): security updates are released for major versions that have had releases in the last year and for the versions of Pebble bundled with maintained Juju releases. Long Term Support (LTS) releases receive five years of standard support, with up to ten additional years of [extended support](https://ubuntu.com/security/esm); other releases leave security maintenance once they fall outside the window above.

To verify the installed version of the daemon, run `pebble version`.


## Cryptographic technology

Pebble uses cryptography in a small number of well-defined places: hashing passwords for the basic identity type, and TLS for the optional HTTPS API. FIPS 140-compliant builds are distributed separately and are described below.

### Basic identity type

For the "basic" [identity](/reference/identities) type, Pebble uses Ulrich Drepper's SHA-crypt algorithm with SHA-512 (a 512-bit digest). Specifically, we use the third party Go library [github.com/GehirnInc/crypt](https://github.com/GehirnInc/crypt) for verifying the password hashes sent in a client's `Authorization` HTTP header.

### TLS

Pebble uses the TLS code in Go's standard library when the `--https` argument is passed to `pebble run`, enabling API access over TLS. Pebble negotiates TLS 1.2 or TLS 1.3; older protocol versions are not offered.

Server-side TLS certificates are managed by Pebble. On first start, a Pebble identity certificate is generated using Ed25519 (256-bit keys). Incoming HTTPS requests will use ephemeral TLS certificates, self-signed with the identity certificate. There is currently no support for integration with an external certificate authority.

Currently, the Pebble client doesn't support HTTPS (TLS). To connect to a Pebble daemon over HTTPS, you'll need to make [API](/reference/api) calls using `curl --insecure`, for example.

Our intention is that projects that build on Pebble can [override how TLS connections are verified](https://pkg.go.dev/github.com/canonical/pebble@v1.23.0/client#Config).

### FIPS 140

This project also distributes [FIPS 140](https://en.wikipedia.org/wiki/FIPS_140)-compliant builds of Pebble: the source code is in the `fips` branch and there's the `fips` track for the official [`pebble` snap](https://snapcraft.io/pebble). These builds take the conservative approach of removing access to cryptographic primitives entirely, pending FIPS validation of the Go runtime: basic-auth password hashing is removed (and so is the third-party SHA-crypt library, so only pre-hashed password identities can authenticate), the `--https` flag is unavailable so the daemon will not serve HTTPS, and outbound requests from the CLI, checks, and log targets are restricted to HTTP (HTTPS origins and HTTP-to-HTTPS redirects fail). Refer to [HACKING.md](https://github.com/canonical/pebble/blob/fips/HACKING.md#fips-140-changes) in the `fips` branch for the authoritative list of changes.

### Cryptographic packages

The cryptographic functionality Pebble uses is provided by two sources. The first is the Go standard library, which supplies TLS and certificate handling (`crypto/tls`, `crypto/x509`), key generation and signing for the identity certificate (`crypto/ed25519`, `crypto/rand`), and SHA-512 hashing (`crypto/sha512`). The second is the third-party [github.com/GehirnInc/crypt](https://github.com/GehirnInc/crypt) library, which provides the SHA-crypt password hashing used for the basic identity type. The FIPS builds remove these cryptographic primitives rather than replacing them; see [](#fips-140) for the resulting feature restrictions.

### Encryption at rest

Pebble does not encrypt state at rest. Confidentiality at rest relies on the `$PEBBLE` directory permissions and on the host's at-rest encryption story. When `$PEBBLE_PERSIST=never` is set, state is held only in memory and the persistence concern does not apply.


## Logging and monitoring

Pebble emits three streams of logs, each with a distinct purpose.

**Daemon logs.** The Pebble daemon writes its own log messages to its standard error stream as one line per message, where each line carries an ISO-8601 timestamp, a level marker, and a free-text message (for example, `2026-06-24T08:00:00.000Z [pebble] HTTP API server listening on ":4000".`). Two levels are emitted: `Notice` is always on and is intended to be user-visible; `Debug` is gated on the `PEBBLE_DEBUG=1` environment variable. The host's service manager or container runtime collects the stream — there is no separate log file or rotation managed by Pebble itself.

Interleaved with these free-text lines, Pebble also emits security events on the same stream, following the [OWASP application logging vocabulary](https://cheatsheetseries.owasp.org/cheatsheets/Logging_Vocabulary_Cheat_Sheet.html). Each event is a single JSON object with `type` set to `security` and the fields `datetime` (RFC 3339, UTC), `level` (`WARN` or `CRITICAL`), `event` (the OWASP event name, optionally suffixed with `:<arg>` — for example `authz_fail:42,/v1/services`), `description`, and `appid` (defaults to `pebble`). The events Pebble currently emits are `sys_startup` and `sys_shutdown` for daemon lifecycle, `sys_monitor_disabled` when service health monitoring is disabled, `authz_admin` and `authz_fail` for authorisation decisions (including TLS client-certificate verification failures), and `user_created`, `user_updated`, and `user_deleted` for changes to the identities database. Because these events ride on the daemon stream, collecting daemon logs is sufficient to collect them; filter on `"type":"security"` to extract them downstream.

**Service logs.** The standard output and standard error of the services Pebble manages are captured and made available, including as a stream, through the logs API and the [`pebble logs`](#reference_pebble_logs_command) command. Passing `--verbose` (or setting `PEBBLE_VERBOSE=1`) on `pebble run` additionally mirrors service output onto the daemon's own standard output. Service logs can be forwarded to external collectors (for example, Loki) through the log-target configuration in the layer plan.

**Notices.** Pebble also records notices, such as warnings and change updates, which clients can list and wait on through the notices API; for more information, see [](/reference/notices).


## Decommissioning

To remove Pebble, uninstall it using the method it was installed with, such as `sudo snap remove pebble` for the snap. If you no longer need the daemon's state, also remove the `$PEBBLE` directory (`/var/lib/pebble/default` by default). This directory can contain identity secrets and service state, so treat its removal as you would any other deletion of sensitive data. On a FIPS deployment, remove the FIPS-build snap and its `$PEBBLE` directory in the same way.


## Reporting vulnerabilities

Security vulnerabilities in Pebble should be reported privately. References:

* [SECURITY.md](https://github.com/canonical/pebble/blob/master/SECURITY.md) for the disclosure process and the supported-version policy.
* The [Ubuntu Security disclosure and embargo policy](https://ubuntu.com/security/disclosure-policy).
* The [GitHub Security Advisories for `canonical/pebble`](https://github.com/canonical/pebble/security/advisories) for known vulnerabilities and their fixes.
* The [Pebble release notes](https://github.com/canonical/pebble/releases) on GitHub.
* Relevant [Ubuntu Security Notices](https://ubuntu.com/security/notices) when a vulnerability also affects an Ubuntu-packaged copy of Pebble.
