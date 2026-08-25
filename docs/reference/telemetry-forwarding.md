# Telemetry forwarding

Pebble supports forwarding its services' logs, metrics and traces to centralized observability systems.

- **Logs** are captured from services' `stdout` and `stderr`, or sent directly by a service using the OpenTelemetry protocol (OTLP). Configure the `log-targets` section of the plan to forward logs to Loki, an OpenTelemetry endpoint or a Syslog server.
- **Metrics** are sent by a service to Pebble using OTLP. Configure the `metrics-targets` section of the plan to forward metrics to an OpenTelemetry endpoint.
- **Traces** are sent by a service to Pebble using OTLP. Configure the `trace-targets` section of the plan to forward traces to an OpenTelemetry endpoint.

Logs, metrics and traces are configured independently, but the three target types share the same overall shape, and the same rules for specifying `services` and `labels` (see below).

(log_forwarding_usage)=
## Log forwarding

In the `log-targets` section of the plan, you can optionally specify a list of remote log destinations where the service logs will be sent:

```yaml
log-targets:
  <log target name>:
    override: merge | replace
    type: loki
    location: <url>
    services: [<service names>]
    labels:
      <label name>: <label value>
    headers:
      <header name>: <header value>
```

Required configuration:

- `override`: How this log target definition is combined with other pre-existing definitions with the same name in the plan. Supported values are `merge` and `replace`.
- `type`: The type of log target. Supported types are `loki`, `opentelemetry` and `syslog`.
- `location`: The URL of the remote log target. For Loki, this needs to be the fully-qualified URL of the push API, including the API endpoint; use the format `http://<host-or-ip>:3100/loki/api/v1/push`. For OpenTelemetry, include the TCP port (normally 4318) without the API endpoint, for example: `http://<host-or-ip>:4318`. For `syslog`, this needs to be either `tcp://<host-or-ip>:<port>` or `udp://<host-or-ip>:<port>`.

Optional configuration:

- `services`: A list of services whose logs will be sent to this target. See {ref}`telemetry_forwarding_specify_services` below.
- `labels`: A list of key/value pairs defining extra labels which should be set on the outgoing logs. See {ref}`telemetry_forwarding_labels` below.
- `headers`: A list of key/value pairs defining extra HTTP headers to send with each request to the log target (for example, `Authorization`). Only supported for the `opentelemetry` target type.

(telemetry_forwarding_metrics_usage)=
## Metrics forwarding

In the `metrics-targets` section of the plan, you can optionally specify a list of remote metrics destinations where the service metrics will be sent:

```yaml
metrics-targets:
  <metrics target name>:
    override: merge | replace
    type: opentelemetry
    location: <url>
    services: [<service names>]
    labels:
      <label name>: <label value>
    headers:
      <header name>: <header value>
```

Required configuration:

- `override`: How this metrics target definition is combined with other pre-existing definitions with the same name in the plan. Supported values are `merge` and `replace`.
- `type`: The type of metrics target. Currently, the only supported type is `opentelemetry`.
- `location`: The URL of the remote metrics target. This needs to include the TCP port (normally 4318) without the API endpoint, for example: `http://<host-or-ip>:4318`.

Optional configuration:

- `services`: A list of services whose metrics will be sent to this target. See {ref}`telemetry_forwarding_specify_services` below.
- `labels`: A list of key/value pairs defining extra labels which should be set on the outgoing metrics. See {ref}`telemetry_forwarding_labels` below.
- `headers`: A list of key/value pairs defining extra HTTP headers to send with each request to the metrics target (for example, `Authorization`).

For a service's metrics to actually reach Pebble in the first place, the service needs to send them to Pebble's local OTLP receiver, as described in {ref}`telemetry_forwarding_otlp_receiver` below.

(telemetry_forwarding_traces_usage)=
## Trace forwarding

In the `trace-targets` section of the plan, you can optionally specify a list of remote trace destinations where the service traces will be sent:

```yaml
trace-targets:
  <trace target name>:
    override: merge | replace
    type: opentelemetry
    location: <url>
    services: [<service names>]
    labels:
      <label name>: <label value>
    headers:
      <header name>: <header value>
```

Required configuration:

- `override`: How this trace target definition is combined with other pre-existing definitions with the same name in the plan. Supported values are `merge` and `replace`.
- `type`: The type of trace target. Currently, the only supported type is `opentelemetry`.
- `location`: The URL of the remote trace target. This needs to include the TCP port (normally 4318) without the API endpoint, for example: `http://<host-or-ip>:4318`.

Optional configuration:

- `services`: A list of services whose traces will be sent to this target. See {ref}`telemetry_forwarding_specify_services` below.
- `labels`: A list of key/value pairs defining extra labels which should be set on the outgoing traces. See {ref}`telemetry_forwarding_labels` below.
- `headers`: A list of key/value pairs defining extra HTTP headers to send with each request to the trace target (for example, `Authorization`).

For a service's traces to actually reach Pebble in the first place, the service needs to send them to Pebble's local OTLP receiver, as described in {ref}`telemetry_forwarding_otlp_receiver` below.

For more details on any of the fields above, see [layer specification](../reference/layer-specification).

(telemetry_forwarding_otlp_receiver)=
## Sending metrics and traces to Pebble

Pebble captures service logs directly, by reading each service's `stdout` and `stderr`. There's no equivalent mechanism for metrics and traces, so instead Pebble runs a local OTLP/HTTP receiver, and each service is responsible for pushing its own metrics and traces to it (typically using an OpenTelemetry SDK). Pebble then buffers and forwards this data to any `metrics-targets` or `trace-targets` the service is enrolled in.

The receiver exposes one endpoint per service and per signal:

- `POST /v1/services/<service name>/otlp/v1/logs`
- `POST /v1/services/<service name>/otlp/v1/metrics`
- `POST /v1/services/<service name>/otlp/v1/traces`

Each endpoint accepts a standard OTLP/HTTP export request, encoded as either JSON (`Content-Type: application/json`) or Protobuf (`Content-Type: application/x-protobuf`), up to 4MiB per request. For security, these endpoints are only reachable over a Unix domain socket, or over plain HTTP from a loopback address; they can't be reached from outside the host.

### Automatic environment variable configuration

To make it easy for a service using an OpenTelemetry SDK to send its telemetry to Pebble, Pebble automatically sets the standard `OTEL_*` environment variables recognized by OpenTelemetry SDKs, for any service enrolled in an `opentelemetry`-type target:

| Signal  | Enrolled in                          | Environment variables set                                                                                                                                          |
|---------|---------------------------------------|----------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| Logs    | an `opentelemetry` log target         | `OTEL_EXPORTER_OTLP_LOGS_ENDPOINT`, `OTEL_EXPORTER_OTLP_LOGS_PROTOCOL=http/protobuf`, `OTEL_LOGS_EXPORTER=otlp`, `OTEL_SERVICE_NAME`                                 |
| Metrics | a metrics target                      | `OTEL_EXPORTER_OTLP_METRICS_ENDPOINT`, `OTEL_EXPORTER_OTLP_METRICS_PROTOCOL=http/protobuf`, `OTEL_METRICS_EXPORTER=otlp`, `OTEL_SERVICE_NAME`                        |
| Traces  | a trace target                        | `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT`, `OTEL_EXPORTER_OTLP_TRACES_PROTOCOL=http/protobuf`, `OTEL_TRACES_EXPORTER=otlp`, `OTEL_SERVICE_NAME`                           |

The `*_ENDPOINT` variable is set to the appropriate `/v1/services/<service name>/otlp/v1/<signal>` URL on Pebble's local receiver, and `OTEL_SERVICE_NAME` is set to the Pebble service name. A service is "enrolled" in a target when it's matched by that target's `services` list (see {ref}`telemetry_forwarding_specify_services` below).

These variables are only set if they haven't already been set in the service's `environment` map, so you can always override any of them (or set `OTEL_LOGS_EXPORTER: none`, `OTEL_METRICS_EXPORTER: none` or `OTEL_TRACES_EXPORTER: none` to opt a service out, for example).

Because enrollment can change how a service's environment is configured, changing a service's enrollment in a `log-targets`, `metrics-targets` or `trace-targets` `services` list causes Pebble to restart that service on the next `pebble replan`.

(telemetry_forwarding_specify_services)=
## Specify services

For each log, metrics or trace target, use the `services` key to specify a list of services to collect telemetry from.

If `services` is not configured, no logs, metrics or traces will be forwarded.

> **Tip:** Use the special keyword `all` to match all services, including services that might be added in future layers.

When merging targets of the same type and name, the `services` lists are appended. Prefix a service name with a minus (for example, `-svc1`) to remove a previously added service. `-all` will remove all services.

(telemetry_forwarding_labels)=
## Labels

Pebble automatically adds some default labels (OpenTelemetry calls these "resource attributes") to outgoing telemetry, in addition to any custom labels configured for the target:

- For `log-targets`:
  - `loki` targets: a `pebble_service` label, set to the service name.
  - `opentelemetry` targets: a `service.name` label, set to the service name.
  - `syslog` targets: the syslog `APP-NAME` field is set to the service name (this isn't a label, but serves the same purpose).
- For `metrics-targets` and `trace-targets` (`opentelemetry` only):
  - `service.name`: always set to the service name, overriding any value the service's OpenTelemetry SDK may have set.
  - `pebble.service`: always set to the service name.
  - `service.instance.id`: a random ID generated by Pebble each time the service (re)starts. Only set if the service's own SDK hasn't already supplied a value.

In the `labels` section, you can optionally specify custom labels to be added to any outgoing logs, metrics or traces. Custom labels always take precedence over an existing attribute with the same key. Label names can't start with the reserved prefix `pebble_`.

The label values may contain `$ENV_VARS`, which will be interpolated using the environment variables for the corresponding service.

## See more

- [How to forward logs to Loki](/how-to/forward-logs-to-loki)
- [How to forward telemetry to an OpenTelemetry endpoint](/how-to/forward-telemetry-to-opentelemetry)
