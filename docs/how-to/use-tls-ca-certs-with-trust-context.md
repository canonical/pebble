# How to use TLS CA certificates with trust contexts

Services, checks, and log targets often need to talk to a server over TLS.
For example, a service calling an internal HTTPS API, an `http` check probing an
HTTPS endpoint, or a `loki`/`opentelemetry` log target pushing logs to a
collector. If that server's certificate is signed by a CA that isn't in the
host's system CA pool (e.g. a private/internal CA), the TLS handshake will fail
unless Pebble is told to trust that CA.

Pebble solves this with **trust contexts**: named collections of trusted CA
certificates, configured once in the plan and then referenced by name wherever
they're needed.

This guide shows how to declare a trust context with a custom CA certificate,
and how to use it from a service, a check, a log target, and `pebble exec`.

## Trust context basics

Trust contexts are declared in the plan's top-level `trust-contexts` section:

```yaml
trust-contexts:
    vendorA:
        override: merge
        tls:
            ca-cert: |
                -----BEGIN CERTIFICATE-----
                ...
                -----END CERTIFICATE-----
```

Two trust contexts are always available, even if you don't declare anything:

- `system`: an immutable trust context backed by the host's default CA
  certificate pool. This name is reserved; you cannot declare your own trust
  context called `system`.
- `default`: the trust context used automatically by any service, check, or
   log target that doesn't set `trust-context` explicitly. The "default" trust
   context simply includes `system`, but you can alter it in your plan (see
   [Change what "default" trusts](#change-what-default-trusts) below).

For the full specification, see [Layer specification](../reference/layer-specification).

## Add a custom CA certificate

Suppose you run an internal HTTPS service (`https://api.internal.example.com`)
whose certificate is signed by an internal CA, `vendorA`. Add a trust context
with that CA certificate:

```yaml
# ca-cert-layer.yaml
trust-contexts:
    vendorA:
        override: merge
        tls:
            ca-cert: |
                -----BEGIN CERTIFICATE-----
                MIIDXTCCAkWgAwIBAgIJAJC1H...
                -----END CERTIFICATE-----
```

`ca-cert` accepts one or more PEM-encoded certificates concatenated together,
so you can include an entire chain (intermediate plus root) if needed.

Add the layer:

```{terminal}
pebble add ca-certs ca-cert-layer.yaml

Layer "ca-certs" added successfully from "ca-cert-layer.yaml"
```

At this point, `vendorA` is declared but not used by anything yet.

## Use a trust context in a service

To make a service trust `vendorA`'s CA bundle, set `trust-context` on the
service:

```yaml
services:
    myservice:
        override: merge
        command: /usr/bin/myservice
        trust-context: vendorA
```

When `myservice` starts, the `vendorA` trust context is resolved and sets the
`SSL_CERT_FILE` environment variable to point at a PEM bundle containing
`vendorA`'s CA certificate(s). Most TLS libraries (including Go's `crypto/x509`
and OpenSSL-based stacks) honor `SSL_CERT_FILE` automatically, so `myservice`
should trust `https://api.internal.example.com` without any code changes.

If `myservice` sets its own `SSL_CERT_FILE` in `environment`, that value is
not modified.

## Use a trust context in a check

### HTTP check

For an `http` check that probes an HTTPS URL signed by `vendorA`, set
`trust-context` on the check's `http` section:

```yaml
checks:
    api-up:
        override: replace
        http:
            url: https://api.internal.example.com/health
            trust-context: vendorA
```

The resolved CA pool is used to validate the server's certificate when
performing the check.

### Exec check

For an `exec` check that runs a command needing to make its own TLS connections,
set `trust-context` on the check's `exec` section, the same way as for services:

```yaml
checks:
    api-check:
        override: replace
        exec:
            command: /usr/bin/check-api.sh
            trust-context: vendorA
```

As with services, this sets `SSL_CERT_FILE` environment variable (unless it's
already set).

## Use a trust context for a log target

If your Loki or OpenTelemetry collector uses a certificate signed by `vendorA`,
set `trust-context` on the log target:

```yaml
log-targets:
    loki-internal:
        override: merge
        type: loki
        location: https://loki.internal.example.com:3100/loki/api/v1/push
        services: [all]
        trust-context: vendorA
```

`trust-context` is only supported for `loki` and `opentelemetry` log targets.

## Use a trust context with `pebble exec`

`pebble exec --context <service-name>` inherits environment variables,
user/group, and working directory from the named service. If that service has a
`trust-context` configured, the trust context is inherited too:

```{terminal}
pebble exec --context myservice curl https://api.internal.example.com/health

{"status": "ok"}
```

Without `--context`, `pebble exec` uses the `default` trust context (typically
just the system CA pool), so an unrelated internal CA won't be trusted unless
you add it to `default` (see below).

(change-what-default-trusts)=
## Change what "default" trusts

If most things in your plan need to trust `vendorA`, it may be simpler to add
it to the `default` trust context instead of setting `trust-context` everywhere.
The `default` trust context can only use `include` (i.e. it can't declare its
own `tls.ca-cert` directly):

```yaml
trust-contexts:
    default:
        override: merge
        include: [system, vendorA]
```

With this in place, any service, check, log target or `pebble exec` that doesn't
set `trust-context` explicitly will trust `vendorA`'s CA in addition to the
system CA pool.

## Combine multiple CAs

A trust context can include other trust contexts, so you can build up a combined
set of trusted CAs. For example, to trust both `vendorA` and `vendorB`:

```yaml
trust-contexts:
    vendorA:
        override: merge
        tls:
            ca-cert: |
                -----BEGIN CERTIFICATE-----
                ...
                -----END CERTIFICATE-----
    vendorB:
        override: merge
        tls:
            ca-cert: |
                -----BEGIN CERTIFICATE-----
                ...
                -----END CERTIFICATE-----
    combined:
        override: merge
        include: [vendorA, vendorB]

services:
    myservice:
        override: merge
        command: /usr/bin/myservice
        trust-context: combined
```

Use the built-in name `system` in an `include` list to add the host's system CA
pool alongside your custom CAs:

```yaml
trust-contexts:
    combined:
        override: merge
        include: [system, vendorA, vendorB]
```

## See more

- [Layer specification](../reference/layer-specification)
- [Health checks](../reference/health-checks)
- [Log forwarding](../reference/log-forwarding)
