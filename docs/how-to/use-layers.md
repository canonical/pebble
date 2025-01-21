# How to use layers

Managing multiple services across different environments becomes complex as systems scale. Pebble simplifies this with layered configurations, improving readability and maintainability. A base layer defines common settings (like logging), while additional layers handle specific services or environment-specific overrides. This declarative approach, along with delegated layer management (for example, operations team managing base layers for logging, service teams managing their services layers), allows for better cross-team collaboration and provides a clear view of each environment's configuration.

## Pebble layers

A layer is a configuration file that defines the desired state of services running on a system. 

Layers are organized within a `layers/` subdirectory in the `$PEBBLE` directory, and their filenames are similar to `001-base-layer.yaml`, where the numerically prefixed filenames ensure a specific order of the layers, and the labels after the prefix uniquely identifies the layer. For example, `001-base-layer.yaml`, `002-override-layer.yaml`.

A layer specifies service properties like the command to execute, startup behaviour, dependencies on other services, and environment variables. For example:

```yaml
summary: Simple layer

description: |
    A better description for a simple layer.

services:
    srv1:
        override: replace
        summary: Service summary
        command: cmd arg1 "arg2a arg2b"
        startup: enabled
        after:
            - srv2
        before:
            - srv3
        requires:
            - srv2
            - srv3
        environment:
            VAR1: val1
            VAR2: val2
            VAR3: val3

    srv2:
        override: replace
        startup: enabled
        command: cmd
        before:
            - srv3

    srv3:
        override: replace
        command: cmd
```

For full details of all fields, see [layer specification](../reference/layer-specification).

## Layer override

Each layer can define new services or modify existing ones defined in preceding layers. The mandatory `override` field in each service definition determines how the layer's configuration interacts with the previously defined service of the same name (if any):

- `override: replace` completely replaces the previous definition of a service.
- `override: merge` combines the current layer's settings with the existing ones, allowing for incremental modifications.

Any of the fields can be replaced individually in a merged service configuration. To illustrate, here is a sample override layer that might sit on top of the one defined in the previous section:

```{code-block} yaml
:emphasize-lines: 5-11,15-16,19-26

summary: Simple override layer

services:
    srv1:
        override: merge
        environment:
            VAR3: val3
        after:
            - srv4
        before:
            - srv5

    srv2:
        override: replace
        summary: Replaced service
        startup: disabled
        command: cmd

    srv4:
        override: replace
        command: cmd
        startup: enabled

    srv5:
        override: replace
        command: cmd
```

See the [full layer specification](../reference/layer-specification) for more details.

## Use layers to manage services

If we are to manage multiple services and environments, we can use a base layer to define common settings like logging, and other layers to define services.

For example, if we have a few teams and each owns different services:

- The operations team: a test Loki server and a staging Loki server (centralized logging systems).- Team foo: `svc1` and `svc2`, whose logs need to be forwarded to the test Loki server.
- Team bar: `svc3` and `svc4`, whose logs need to be forwarded to the staging Loki server.

The operations team can define a base layer named `001-base-layer.yaml` with multiple log targets:

```yaml
summary: a base layer for log targets
log-targets:
  test:
    override: merge
    type: loki
    location: http://my-test-loki-server:3100/loki/api/v1/push
    services: [svc1, svc2]
    labels:
      owner: '$OWNER'
      env: 'test'
  staging:
    override: merge
    type: loki
    location: http://my-staging-loki-server:3100/loki/api/v1/push
    services: [svc3, svc4]
    labels:
      owner: '$OWNER'
      env: 'staging'
```

For more information on log targets and log forwarding, see [How to forward logs to Loki](./forward-logs-to-loki).

Team foo can define another layer named `002-foo.yaml` without worrying about the log targets:

```yaml
summary: layer managed by team foo
services:
  svc1:
    override: replace
    command: cmd
    startup: enabled
    environment:
      OWNER: 'foo'
  svc2:
    override: replace
    command: cmd
    startup: enabled
    environment:
      OWNER: 'foo'
```

Team bar can define yet another layer named `003-bar.yaml`:

```yaml
summary: layer managed by team bar
services:
  svc3:
    override: replace
    command: cmd
    startup: enabled
    environment:
      OWNER: 'bar'
  svc4:
    override: replace
    command: cmd
    startup: enabled
    environment:
      OWNER: 'bar'
```

In this way, logs for `svc1` and `svc2` managed by team foo ar forwarded to the test Loki, and logs for `svc3` and `svc4` managed by team bar are forwarded to the staging Loki, all with corresponding labels attached.

## See more

- [Layer specification](/reference/layer-specification.md)
