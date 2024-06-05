# How to use log forwarding

Pebble supports forwarding its services' logs to a remote Loki server. In the `log-targets` section of the plan, you can specify destinations for log forwarding, for example:
```yaml
log-targets:
    staging-logs:
        override: merge
        type: loki
        location: http://10.1.77.205:3100/loki/api/v1/push
        services: [all]
    production-logs:
        override: merge
        type: loki
        location: http://my.loki.server.com/loki/api/v1/push
        services: [svc1, svc2]
```

## Specifying services

For each log target, use the `services` key to specify a list of services to collect logs from. In the above example, the `production-logs` target will collect logs from `svc1` and `svc2`.

Use the special keyword `all` to match all services, including services that might be added in future layers. In the above example, `staging-logs` will collect logs from all services.

To remove a service from a log target when merging, prefix the service name with a minus `-`. For example, if we have a base layer with
```yaml
my-target:
    services: [svc1, svc2]
```
and override layer with
```yaml
my-target:
    services: [-svc1]
    override: merge
```
then in the merged layer, the `services` list will be merged to `[svc1, svc2, -svc1]`, which evaluates left to right as simply `[svc2]`. So `my-target` will collect logs from only `svc2`.

You can also use `-all` to remove all services from the list. For example, adding an override layer with
```yaml
my-target:
    services: [-all]
    override: merge
```
would remove all services from `my-target`, effectively disabling `my-target`. Meanwhile, adding an override layer with
```yaml
my-target:
    services: [-all, svc1]
    override: merge
```
would remove all services and then add `svc1`, so `my-target` would receive logs from only `svc1`.

## Labels

In the `labels` section, you can specify custom labels to be added to any outgoing logs. These labels may contain `$ENVIRONMENT_VARIABLES` - these will be interpreted in the environment of the corresponding service. Pebble may also add its own default labels (depending on the protocol). For example, given the following plan:
```yaml
services:
  svc1:
    environment:
      OWNER: 'alice'
  svc2:
    environment:
      OWNER: 'bob'

log-targets:
  tgt1:
    type: loki
    labels:
      product: 'juju'
      owner: 'user-$OWNER'
```
the logs from `svc1` will be sent with the following labels:
```yaml
product: juju
owner: user-alice     # env var $OWNER substituted
pebble_service: svc1  # default label for Loki
```
and for svc2, the labels will be
```yaml
product: juju
owner: user-bob       # env var $OWNER substituted
pebble_service: svc2  # default label for Loki
```
