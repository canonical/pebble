# How to forward telemetry to an OpenTelemetry endpoint

Centralized observability aggregates logs, metrics and traces from different sources into a unified platform, simplifying analysis by providing a central view of an app. For instance, when a single request goes through multiple services, it's much easier to troubleshoot with centralized logs, metrics and traces.

This guide demonstrates how to forward Pebble's logs, metrics and traces to an [OpenTelemetry](https://opentelemetry.io/) endpoint, using the OpenTelemetry protocol (OTLP) over HTTP.

> Note: Pebble captures logs automatically from each service's `stdout` and `stderr`, but there's no equivalent mechanism for metrics and traces. Those signals must be pushed to Pebble by the service itself (typically via an OpenTelemetry SDK). See {ref}`send_metrics_and_traces_to_pebble` below for how Pebble makes this easy.

## Set up an OpenTelemetry Collector

For testing, the easiest way is to run the [OpenTelemetry Collector](https://opentelemetry.io/docs/collector/) locally, configured with an OTLP/HTTP receiver and a `debug` exporter that prints everything it receives to the console.

1. Download the latest [`otelcol-contrib` release](https://github.com/open-telemetry/opentelemetry-collector-releases/releases/latest) binary for your operating system and architecture.
1. Create a config file named `otel-collector-config.yaml`:

   ```yaml
   receivers:
     otlp:
       protocols:
         http:
           endpoint: 0.0.0.0:4318

   exporters:
     debug:
       verbosity: detailed

   service:
     pipelines:
       logs:
         receivers: [otlp]
         exporters: [debug]
       metrics:
         receivers: [otlp]
         exporters: [debug]
       traces:
         receivers: [otlp]
         exporters: [debug]
   ```

1. Run the collector: `otelcol-contrib --config otel-collector-config.yaml`.

The collector is now listening for OTLP/HTTP requests on `http://localhost:4318`, and will print any logs, metrics or traces it receives.

For more information, see [Install the Collector](https://opentelemetry.io/docs/collector/installation/) and [Collector configuration](https://opentelemetry.io/docs/collector/configuration/).

## Forward service logs to an OpenTelemetry endpoint

To forward logs to the OpenTelemetry endpoint running at `http://localhost:4318`, use the following config:

```yaml
log-targets:
  otel:
    override: merge
    type: opentelemetry
    location: http://localhost:4318
    services: [all]
```

This creates a log target named "otel" and will forward logs from all services. Pebble sends each service's logs with a `service.name` resource attribute set to the service name.

For more information on log forwarding and `log-targets` configuration, see {ref}`log_forwarding_usage`.

## Forward service metrics to an OpenTelemetry endpoint

To forward metrics to the same OpenTelemetry endpoint, use the following config:

```yaml
metrics-targets:
  otel:
    override: merge
    type: opentelemetry
    location: http://localhost:4318
    services: [all]
```

This creates a metrics target named "otel" and enrolls all services in it. However, unlike logs, this alone doesn't produce any metrics: each enrolled service must still send its metrics to Pebble.

To make this easy, Pebble runs a local OTLP/HTTP receiver, and automatically sets the standard `OTEL_EXPORTER_OTLP_METRICS_*` environment variables (recognized by every OpenTelemetry SDK) in the environment of any service enrolled in a metrics target. For example, once service `svc1` is enrolled above, Pebble sets:

```text
OTEL_EXPORTER_OTLP_METRICS_ENDPOINT=http://<pebble-address>/v1/services/svc1/otlp/v1/metrics
OTEL_EXPORTER_OTLP_METRICS_PROTOCOL=http/protobuf
OTEL_METRICS_EXPORTER=otlp
OTEL_SERVICE_NAME=svc1
```

If `svc1`'s process uses an OpenTelemetry SDK with standard environment variable auto-configuration (most languages' SDKs support this out of the box, with no code changes required), it will automatically start sending its metrics to Pebble, which then forwards them on to the "otel" target.

> Note: If `svc1` is already running when you add it to a metrics target, run `pebble replan` to restart it and pick up the new environment variables.

For more information on how Pebble receives metrics from services and forwards them, see {ref}`telemetry_forwarding_metrics_usage` and {ref}`send_metrics_and_traces_to_pebble`.

## Forward service traces to an OpenTelemetry endpoint

Traces work the same way as metrics. To forward traces to the OpenTelemetry endpoint, use the following config:

```yaml
trace-targets:
  otel:
    override: merge
    type: opentelemetry
    location: http://localhost:4318
    services: [all]
```

As with metrics, Pebble automatically sets the standard `OTEL_EXPORTER_OTLP_TRACES_*` environment variables for any service enrolled in a trace target:

```text
OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=http://<pebble-address>/v1/services/svc1/otlp/v1/traces
OTEL_EXPORTER_OTLP_TRACES_PROTOCOL=http/protobuf
OTEL_TRACES_EXPORTER=otlp
OTEL_SERVICE_NAME=svc1
```

So an instrumented service exporting spans via its OpenTelemetry SDK will automatically send them to Pebble, which forwards them on to the "otel" target.

For more information, see {ref}`telemetry_forwarding_traces_usage`.

(send_metrics_and_traces_to_pebble)=
## Send metrics and traces to Pebble manually

If a service doesn't use an OpenTelemetry SDK, or you'd rather not rely on environment variable auto-configuration, you (or a sidecar) can push OTLP data directly to Pebble's local receiver, at:

```text
POST /v1/services/<service name>/otlp/v1/metrics
POST /v1/services/<service name>/otlp/v1/traces
```

These endpoints accept a standard `ExportMetricsServiceRequest` or `ExportTraceServiceRequest`, encoded as either JSON (`Content-Type: application/json`) or Protobuf (`Content-Type: application/x-protobuf`). They're only reachable over a Unix domain socket, or over plain HTTP from a loopback address, so they can only be used by processes running alongside Pebble on the same host.

For more details, see {ref}`telemetry_forwarding_otlp_receiver`.

## Verify telemetry is being forwarded

With the collector from the previous section running, and services enrolled in the "otel" log, metrics and trace targets, the collector's console output should show entries similar to (trimmed for brevity):

```{terminal}
otelcol-contrib --config otel-collector-config.yaml

LogRecord #0
Timestamp: 2025-01-02 00:22:44.000 +0000
Body: Str(* Running on http://127.0.0.1:5000)
Resource attributes:
     -> service.name: Str(svc1)
Metric #0
Descriptor:
     -> Name: http.server.duration
Resource attributes:
     -> service.name: Str(svc1)
     -> pebble.service: Str(svc1)
     -> service.instance.id: Str(3c1c5b1e-...)
Span #0
     Trace ID       : 5b8aa5a2d2c872e8321cf37308d69df2
     Name           : handle-request
Resource attributes:
     -> service.name: Str(svc1)
     -> pebble.service: Str(svc1)
```

The exact fields printed depend on the collector version and the data your service emits, but the presence of `service.name` and `pebble.service` attributes confirms Pebble is enriching and forwarding the telemetry correctly.

## Forward to multiple targets

Just like log targets, you can define multiple metrics or trace targets, and select which services forward to each one. For example, to forward metrics to both a local test collector and a staging collector:

```yaml
metrics-targets:
  test:
    override: merge
    type: opentelemetry
    location: http://localhost:4318
    services: [all]
  staging:
    override: merge
    type: opentelemetry
    location: http://my-staging-collector:4318
    services: [all]
```

Or, to specify which services forward to each target:

```yaml
metrics-targets:
  test:
    override: merge
    type: opentelemetry
    location: http://localhost:4318
    services: [svc1, svc2]
  staging:
    override: merge
    type: opentelemetry
    location: http://my-staging-collector:4318
    services: [svc3, svc4]
```

This will forward metrics from `svc1` and `svc2` to the "test" target, and metrics from `svc3` and `svc4` to the "staging" target. The same pattern applies to `log-targets` and `trace-targets`.

## Remove services

To remove a service from a target when merging another layer, prefix the service name with a minus `-`. For example, if we have a base layer with:

```yaml
metrics-targets:
  my-target:
    services: [svc1, svc2]
```

And an override layer with:

```yaml
metrics-targets:
  my-target:
    services: [-svc1]
    override: merge
```

Then in the merged layer, the `services` list will be merged to `[svc2]`, so `my-target` will only collect metrics from `svc2`.

We can also use `-all` to remove all services. For example, adding an override layer with:

```yaml
metrics-targets:
  my-target:
    services: [-all]
    override: merge
```

This would remove all services from `my-target`, effectively disabling it.

To make sure that `my-target` only receives metrics from `svc1`, use this override layer instead:

```yaml
metrics-targets:
  my-target:
    services: [-all, svc1]
    override: merge
```

This works the same way for `log-targets` and `trace-targets`.

For more information, see {ref}`telemetry_forwarding_specify_services`.

## Use labels

Besides the default resource attributes Pebble adds (`service.name`, `pebble.service`, and `service.instance.id` for metrics and traces), you can add custom labels to any target.

For example, to add a label `env` with the value `dev` to a metrics target:

```yaml
metrics-targets:
  otel:
    override: merge
    type: opentelemetry
    location: http://localhost:4318
    services: [all]
    labels:
      env: dev
```

Label values may contain environment variables that are defined for services, letting you add "dynamic" labels - different labels for different services. For example, given the following layer configuration:

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
trace-targets:
  otel:
    override: merge
    type: opentelemetry
    location: http://localhost:4318
    services: [all]
    labels:
      owner: 'user-$OWNER'
```

Traces from `svc1` will be sent with the following resource attributes:

```text
service.name: svc1        # default attribute
pebble.service: svc1      # default attribute
owner: user-alice         # env var $OWNER substituted
```

And for `svc2`:

```text
service.name: svc2        # default attribute
pebble.service: svc2      # default attribute
owner: user-bob           # env var $OWNER substituted
```

This works the same way for `log-targets` and `metrics-targets`.

For more information, see {ref}`telemetry_forwarding_labels`.

## See more

Pebble:

- [Layer specification](../reference/layer-specification)
- [Telemetry forwarding](../reference/telemetry-forwarding)
- [How to forward logs to Loki](./forward-logs-to-loki)

OpenTelemetry:

- [OpenTelemetry Collector documentation](https://opentelemetry.io/docs/collector/)
- [OTLP specification](https://opentelemetry.io/docs/specs/otlp/)
