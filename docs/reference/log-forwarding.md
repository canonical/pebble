# Log forwarding

Pebble supports forwarding its services' logs to centralized logging systems.

(log_forwarding_usage)=
## Usage

In the `log-targets` section of the plan, you can optionally specify a list of remote log destinations where the service logs can be sent:

```yaml
log-targets:
  <log target name>:
    override: merge | replace
    type: loki
    location: <url>
    services: [<service names>]
    labels:
      <label name>: <label value>
```

Mandatory configuration:

- `override`: How this log target definition is combined with other pre-existing definitions with the same name in the plan. Supported values are `merge` and `replace`.
- `type`: The type of log target. The only supported type is `loki`.
- `location`: The URL of the remote log target. For Loki, this needs to be the fully-qualified URL of the push API including the API endpoint in the format of `http://<ip-address>:3100/loki/api/v1/push`.

Optional configuration:

- `services`: A list of services whose logs will be sent to this target. Use the special keyword `all` to match all services in the plan. _Note that although this configuration is non-mandatory, if not set, no logs will be forwarded._
- `labels`: A list of key/value pairs defining extra labels which should be set on the outgoing logs.

For more details, see [layer specification](../reference/layer-specification).

(log_forwarding_specify_services)=
## Specify services

For each log target, use the `services` key to specify a list of services from which to collect logs.

If `services` is not configured, no logs will be forwarded.

Use the special keyword `all` to match all services, _including services that might be added in future layers_.

When merging log targets, the `services` lists are appended. Prefix a service name with a minus (for example, `-svc1`) to remove a previously added service. `-all` will remove all services.

(log_forwarding_labels)=
## Labels

For all outgoing logs, Pebble will set a default label `pebble_service` with the service name.

In the `labels` section, you can optionally specify custom labels to be added to any outgoing logs.

The label values may contain `$ENV_VARS`, which will be interpolated using the environment variables for the corresponding service. _Note that for all outgoing logs, Pebble will set a default label `pebble_service` with the name of the service_.
