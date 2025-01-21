# How to forward logs to Loki

Centralized logging aggregates logs from different sources into a unified platform, simplifying analysis by providing a central view of an app. For instance, when a single request goes through multiple services, it's much easier to troubleshoot with centralized logs.

This guide demonstrates how to forward Pebble logs to the centralized logging system [Grafana Loki](https://grafana.com/oss/loki/).

> Note: The only logging system that Pebble supports is Grafana Loki.

## Set up Loki and LogCLI

### Loki

For testing, the easiest way is to download the latest pre-built binary and run it locally:

1. Find the latest release on the [Loki releases page](https://github.com/grafana/loki/releases/) and download the binary according to your operating system and architecture.
1. Download the sample local config: `wget https://raw.githubusercontent.com/grafana/loki/main/cmd/loki/loki-local-config.yaml`.
1. Run Loki locally: `loki-linux-amd64 -config.file=loki-local-config.yaml`.

For more information, see [Install Grafana Loki locally](https://grafana.com/docs/loki/latest/setup/install/local/). For information about a production-ready setup, see [Get started with Grafana Loki](https://grafana.com/docs/loki/latest/get-started/).

### LogCLI

We'll also install [LogCLI](https://grafana.com/docs/loki/latest/query/logcli/), a command-line tool for querying logs in Loki. Download the `logcli` binary from the [Loki releases page](https://github.com/grafana/loki/releases).

For more information, see [LogCLI installation and reference](https://grafana.com/docs/loki/latest/query/logcli/getting-started/) and [LogCLI tutorial](https://grafana.com/docs/loki/latest/query/logcli/logcli-tutorial/).

## Forward service logs to Loki

To forward logs to a Loki server running at `http://localhost:3100`, use the following config:

```yaml
log-targets:
  test:
    override: merge
    type: loki
    location: http://localhost:3100/loki/api/v1/push
    services: [all]
```

This creates a log target named "test" and will forward logs from all services.

For more information on log forwarding and `log-targets` configuration, see {ref}`log_forwarding_usage`.

To specify which services to forward logs from, list the service names in `services`. For example:

```yaml
    services: [svc1, svc2]
```

For more information on `services` configuration, see {ref}`log_forwarding_specify_services`.

## Query logs in Loki

To verify that logs have been forwarded to Loki, use `logcli`.

First, we can check the existing labels in Loki:

```{terminal}
   :input: logcli labels
http://localhost:3100/loki/api/v1/labels?end=1735748567383845370&start=1735744967383845370
pebble_service
service_name
```

To see the values for label `pebble_service`, run:

```{terminal}
   :input: logcli labels pebble_service
http://localhost:3100/loki/api/v1/label/pebble_service/values?end=1735748583854357586&start=1735744983854357586
svc1
```

To query logs from service `svc1`, run:

```{terminal}
   :input: logcli query '{pebble_service="svc1"}'
http://localhost:3100/loki/api/v1/label/pebble_service/values?end=1735748583854357586&start=1735744983854357586
http://localhost:3100/loki/api/v1/query_range?direction=BACKWARD&end=1735748593047338924&limit=30&query=%7Bpebble_service%3D%22svc1%22%7D&start=1735744993047338924
Common labels: {pebble_service="svc1", service_name="unknown_service"}
2025-01-02T00:22:45+08:00 {detected_level="unknown"} * Debugger PIN: 411-846-353
2025-01-02T00:22:45+08:00 {detected_level="unknown"} * Debugger is active!
2025-01-02T00:22:44+08:00 {detected_level="warn"}    WARNING: This is a development server. Do not use it in a production deployment. Use a production WSGI server instead.
2025-01-02T00:22:44+08:00 {detected_level="unknown"} * Restarting with stat
2025-01-02T00:22:44+08:00 {detected_level="unknown"} Press CTRL+C to quit
2025-01-02T00:22:44+08:00 {detected_level="unknown"} * Running on http://127.0.0.1:5000
2025-01-02T00:22:44+08:00 {detected_level="unknown"} * Debug mode: on
2025-01-02T00:22:44+08:00 {detected_level="unknown"} * Serving Flask app 'main'
http://localhost:3100/loki/api/v1/query_range?direction=BACKWARD&end=1735748564935000001&limit=24&query=%7Bpebble_service%3D%22svc1%22%7D&start=1735744993047338924
```

## Forward logs to multiple targets

If we have a Loki for the test environment running at `http://my-test-loki-server:3100` and another Loki for the staging environment running at `http://my-staging-loki-server:3100`, we can specify two log targets:

```yaml
log-targets:
  test:
    override: merge
    type: loki
    location: http://my-test-loki-server:3100/loki/api/v1/push
    services: [all]
  staging:
    override: merge
    type: loki
    location: http://my-staging-loki-server:3100/loki/api/v1/push
    services: [all]
```

Or, to specify which services to forward logs from:

```yaml
log-targets:
  test:
    override: merge
    type: loki
    location: http://my-test-loki-server:3100/loki/api/v1/push
    services: [svc1, svc2]
  staging:
    override: merge
    type: loki
    location: http://my-staging-loki-server:3100/loki/api/v1/push
    services: [svc3, svc4]
```

This will forward logs from `svc1` and `svc2` to the test target, and logs from `svc3` and `svc4` to the staging target.

## Remove services

To remove a service from a log target when merging another layer, prefix the service name with a minus `-`. For example, if we have a base layer with:

```yaml
my-target:
  services: [svc1, svc2]
```

And an override layer with:

```yaml
my-target:
  services: [-svc1]
  override: merge
```

Then in the merged layer, the `services` list will be merged to `[svc2]`, so `my-target` will collect logs from only `svc2`.

We can also use `-all` to remove all services. For example, adding an override layer with:

```yaml
my-target:
  services: [-all]
  override: merge
```

This would remove all services from `my-target`, effectively disabling `my-target`.

To make sure that `my-target` only receives logs from `svc1`, use this override layer instead:

```yaml
my-target:
  services: [-all, svc1]
  override: merge
```

## Use labels

Besides the default label Pebble adds to all outgoing logs, we can add more labels to service logs.

For example, to add a label `env` with the value `dev` to a log target:

```yaml
log-targets:
  test:
    override: merge
    type: loki
    location: http://my-test-loki-server:3100/loki/api/v1/push
    services: [all]
    labels:
      env: dev
```

The label values may contain environment variables that are defined for services. With this feature, we can add "dynamic" labels - different labels for different services. For example, given the following layer configuration:

```yaml
summary: a simple layer
services:
  svc1:
    override: replace
    command: foo
    environment:
      OWNER: 'alice'
  svc2:
    override: replace
    command: bar
    environment:
      OWNER: 'bob'
log-targets:
  test:
    override: merge
    type: loki
    location: http://localhost:3100/loki/api/v1/push
    services: [all]
    labels:
      owner: 'user-$OWNER'
```

The logs from `svc1` will be sent with the following labels:

```
pebble_service: svc1  # default label
owner: user-alice     # env var $OWNER substituted
```

And for `svc2`, the labels will be:

```
pebble_service: svc2  # default label
owner: user-bob       # env var $OWNER substituted
```

For more information on `labels`, see {ref}`log_forwarding_labels`.

## See more

Pebble:

- [Layer specification](../reference/layer-specification)
- [Log forwarding](../reference/log-forwarding)

Loki:

- [Get started with Grafana Loki](https://grafana.com/docs/loki/latest/get-started/)
- [Setup Loki](https://grafana.com/docs/loki/latest/setup/)
- [LogCLI installation and reference](https://grafana.com/docs/loki/latest/query/logcli/getting-started/)
- [LogCLI tutorial](https://grafana.com/docs/loki/latest/query/logcli/logcli-tutorial/)
