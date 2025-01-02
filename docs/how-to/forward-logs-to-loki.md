# How to forward logs to Loki

Centralized logging aggregates logs from various sources and services into a single, unified platform which simplifies troubleshooting and analysis by providing a centralized view of the application's behavior. This is especially important in microservice architectures. In these distributed systems, a single user request often traverses multiple services, making it incredibly difficult to trace the execution flow and identify the source of errors when relying on individual service logs. Centralized logging addresses this challenge by allowing developers to follow a request's journey across the entire system in one place, making it essential for maintaining observability and improving troubleshooting efficiency.

With Pebble, we can easily configure log forwarding to centralized logging systems.

> Note: At the moment, the only supported logging system is [Grafana Loki](https://grafana.com/oss/loki/).

## Setup Loki and LogCLI

### Loki

For testing purposes, the easiest way is to [download the latest pre-built binary and run it locally](https://grafana.com/docs/loki/latest/setup/install/local/#install-manually):

- Find the latest release on the [releases page](https://github.com/grafana/loki/releases/) and download the binary according to your operating system and architecture.
- Download the sample local config by running: `wget https://raw.githubusercontent.com/grafana/loki/main/cmd/loki/loki-local-config.yaml`.
- Run Loki locally by running: `loki-linux-amd64 -config.file=loki-local-config.yaml`.

For more information on a production-ready setup, refer to the official doc [Get started with Grafana Loki](https://grafana.com/docs/loki/latest/get-started/) and [Setup Loki](https://grafana.com/docs/loki/latest/setup/).

### LogCLI

Besides Loki itself, you may also want to install [LogCLI](https://grafana.com/docs/loki/latest/query/logcli/), which is a command-line tool for querying and exploring logs in Loki. Download the `logcli` binary from the [Loki releases page](https://github.com/grafana/loki/releases).

For more information, see LogCLI installation and reference [here](https://grafana.com/docs/loki/latest/query/logcli/getting-started/) and a tutorial [here](https://grafana.com/docs/loki/latest/query/logcli/logcli-tutorial/).

## Forward all services' logs to Loki

To forward logs to a Loki server running at `http://localhost:3100`, use the following config:

```yaml
log-targets:
  test:
    override: merge
    type: loki
    location: http://localhost:3100/loki/api/v1/push
    services: [all]
```

The configuration above creates a log target named "test" of type Loki with an override policy set to "merge", and it will forward all services' logs to this target.

For more information on log forwarding and `log-targets` configuration, see {ref}`log_forwarding_usage`.

## Query logs in Loki

To confirm logs are successfully forwarded to Loki, use `logcli` to query.

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

## Forward selected services' logs

In the previous sections, we specified `[all]` for `services`, which will forward all logs from all services to the target.

If you want to specify which services' logs to forward explicitly, list service names in `services`. For example:

```yaml
log-targets:
  test:
    override: merge
    type: loki
    location: http://localhost:3100/loki/api/v1/push
    services: [svc1, svc2]
```

This will only forward logs from services `svc1` and `svc2`.

For more information on `services` configuration, see {ref}`log_forwarding_specify_services`.

## Forward logs to multiple targets

We can forward logs to multiple targets.

For example, if we have a Loki for the test environment running at `http://my-test-loki-server:3100` and another Loki for the staging environment running at `http://my-staging-loki-server:3100`, combining with the `services` feature mentioned above, we can specify two log targets:

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

We can also combine this feature with the previously mentioned "service selection" feature to forward different services' logs to different targets. For example:

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

This will forward logs from `svc1` and `svc2` to the test target, and logs from `svc3` and `svc4` will be forwarded to the staging target.

## Remove services

To remove a service from a log target when merging, prefix the service name with a minus `-`. For example, if we have a base layer with:

```yaml
my-target:
  services: [svc1, svc2]
```

And override layer with:

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

Meanwhile, adding an override layer with:

```yaml
my-target:
  services: [-all, svc1]
  override: merge
```

This would remove all services and then add `svc1`, so `my-target` would receive logs from only `svc1`.

## Use labels

Besides the default label Pebble adds to all outgoing logs, we can add more labels to service logs.

For example, to add a label `env` with the value `test` to a log target:

```yaml
log-targets:
  test:
    override: merge
    type: loki
    location: http://my-test-loki-server:3100/loki/api/v1/push
    services: [all]
    labels:
      env: test
```

The label values may contain `$ENV_VARS`, which will be interpolated using the environment variables for the corresponding service. With this feature, we can add "dynamic" labels - different labels for different services. For example, given the following layer configuration:

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

And for svc2, the labels will be:

```
pebble_service: svc2  # default label
owner: user-bob       # env var $OWNER substituted
```

For more information on `labels`, see {ref}`log_forwarding_labels`.

## See more

- [Layer specification](../reference/layer-specification)
- [Log forwarding](../reference/log-forwarding)
- [Grafana Loki](https://grafana.com/oss/loki/)
- [Get started with Grafana Loki](https://grafana.com/docs/loki/latest/get-started/)
- [Setup Loki](https://grafana.com/docs/loki/latest/setup/)
- [LogCLI](https://grafana.com/docs/loki/latest/query/logcli/)
- [LogCLI installation and reference](https://grafana.com/docs/loki/latest/query/logcli/getting-started/)
- [LogCLI tutorial](https://grafana.com/docs/loki/latest/query/logcli/logcli-tutorial/)
