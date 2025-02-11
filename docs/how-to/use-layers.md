# How to use layers

Managing multiple services across different environments becomes complex as systems scale. Pebble simplifies this with layered configurations, improving readability and maintainability.

A base layer defines common settings (such as logging), while additional layers handle specific services or environment-specific overrides. This declarative approach, along with delegated layer management, allows for better cross-team collaboration and provides a clear view of each environment's configuration. For example, an operations team could manage base layers for logging, and service teams could manage layers for their services.

(use_layers_pebble_layers)=
## Pebble layers

A layer is a configuration file that defines the desired state of the managed services.

Layers are organized within a `layers/` subdirectory in the `$PEBBLE` directory. Their filenames are similar to `001-base-layer.yaml`, where the numerically prefixed filenames ensure a specific order of the layers, and the labels after the prefix uniquely identify the layers. For example, `001-base-layer.yaml` and `002-override-layer.yaml`.

A layer can define service properties, health checks, and log targets. For example:

```yaml
services:
  server:
    override: replace
    command: flask --app hello run
    requires:
      - srv2
    environment:
      PORT: 5000
      DATABASE: dbserver.example.com
  database:
    override: replace
    command: postgres -D /usr/local/pgsql/data

checks:
  server-liveness:
    override: replace
    http:
      url: http://127.0.0.1:5000/health
```

For full details of all fields, see [layer specification](../reference/layer-specification).

## Layer override

Each layer can define new services (and health checks and log targets) or modify existing ones defined in preceding layers. The layers -- ordered by numerical prefix -- are combined into the final plan.

The required `override` field in each service (or health check or log target) determines how the layer's configuration interacts with the previously defined object of the same name (if any):

- `override: replace` completely replaces the previous definition of a service.
- `override: merge` combines the current layer's settings with the existing ones, allowing for incremental modifications.

Any of the fields can be replaced individually in a merged service configuration.

For example, the following is an override layer that can be combined with the example layer defined in the previous section:

```{code-block} yaml
:emphasize-lines: 5-6,12

services:
  server:
    override: merge
    environment:
      PORT: 8080
      VERBOSE_LOGGING: 1

checks:
  server-liveness:
    override: replace
    http:
      url: http://127.0.0.1:8080/health
```

And the combined plan will be:

```{code-block} yaml
:emphasize-lines: 8-9,19

services:
  server:
    override: replace
    command: flask --app hello run
    requires:
      - srv2
    environment:
      PORT: 8080
      VERBOSE_LOGGING: 1
      DATABASE: dbserver.example.com
  database:
    override: replace
    command: postgres -D /usr/local/pgsql/data

checks:
  server-liveness:
    override: replace
    http:
      url: http://127.0.0.1:8080/health
```

See the [full layer specification](../reference/layer-specification) for details.

## Add a layer dynamically

The `pebble add` command can dynamically add a layer to the plan's layers.

For example, given the example layer defined in the {ref}`use_layers_pebble_layers` section, if we add the following layer:

```yaml
services:
  a-new-server:
    override: replace
    command: flask --app world run
```

The plan will become:

```{code-block} yaml
:emphasize-lines: 2-4

services:
  a-new-server:
    override: replace
    command: flask --app world run
  server:
    override: replace
    command: flask --app hello run
    requires:
      - srv2
    environment:
      PORT: 8080
      DATABASE: dbserver.example.com
  database:
    override: replace
    command: postgres -D /usr/local/pgsql/data

checks:
  server-liveness:
    override: replace
    http:
      url: http://127.0.0.1:8080/health
```

For more information, see {ref}`reference_pebble_add_command`.


## Use layers to manage services

If we are to manage multiple services and environments, we can use a base layer to define common settings such as logging, and other layers to define services.

For example, suppose that we have some teams that own different services:

- The operations team: a test Loki server and a staging Loki server (centralized logging systems).
- Team foo: `svc1` and `svc2`, whose logs need to be forwarded to the test Loki server.
- Team bar: `svc3` and `svc4`, whose logs need to be forwarded to the staging Loki server.

The operations team can define a base layer named `001-base-layer.yaml` with multiple log targets, and they don't need to worry about which service logs should be forwarded to which log targets. In the base layer, `services: [all]` can be used as a start:

```yaml
log-targets:
  test:
    override: merge
    type: loki
    location: http://my-test-loki-server:3100/loki/api/v1/push
    services: [all]
    labels:
      owner: '$OWNER'
      env: 'test'
  staging:
    override: merge
    type: loki
    location: http://my-staging-loki-server:3100/loki/api/v1/push
    services: [all]
    labels:
      owner: '$OWNER'
      env: 'staging'
```

For more information on log targets and log forwarding, see [How to forward logs to Loki](./forward-logs-to-loki).

Team foo can define another layer named `002-foo.yaml` without having to redefine the log targets. However, they can decide which service logs are forwarded to which targets by overriding the `services` configuration of a predefined log target in the base layer:

```yaml
services:
  svc1:
    override: replace
    command: cmd
    environment:
      OWNER: 'foo'
  svc2:
    override: replace
    command: cmd
    environment:
      OWNER: 'foo'
log-targets:
  test:
    override: merge
    services: [svc1, svc2]
```

Team bar can define yet another layer named `003-bar.yaml`:

```yaml
services:
  svc3:
    override: replace
    command: cmd
    environment:
      OWNER: 'bar'
  svc4:
    override: replace
    command: cmd
    environment:
      OWNER: 'bar'
log-targets:
  staging:
    override: merge
    services: [svc3, svc4]
```

In this way, logs for `svc1` and `svc2` managed by team foo are forwarded to the test Loki, and logs for `svc3` and `svc4` managed by team bar are forwarded to the staging Loki, all with corresponding labels attached. Each team owns its own layer, achieving true cross-team collaboration and delegated layer management.

## See more

- [Layer specification](/reference/layer-specification.md)
