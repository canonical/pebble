# How to use layers

Orchestrating multiple services in remote systems is a common requirement in modern software. Consider a typical web application, which might involve:

- a web server (like Nginx)
- an application server (like uWSGI)
- a database (like PostgreSQL)
- a caching service (like Redis)

Each of these services needs to be configured, started, and stopped in a coordinated manner.

As the system scales, the operational overhead grows exponentially because multiple groups of applications need to be managed, and they need to be deployed in different environments with slightly different configurations. Without proper service orchestration tooling, managing dependencies and ensuring consistent behaviour across multiple services in multiple environments is complex and error-prone. 

With Pebble, it's possible to reduce this operational overhead by splitting service configurations into different layers. Depending on how we make use of this feature, it can offer great advantages, especially when the system scales out:

- We can organize services into logical groups, which greatly improves the readability and maintainability of the configurations. With this declarative, layered approach where we define one set of configurations per environment, it is easier to understand the overall system because in this way, we know exactly what's running in a given environment and we don't have to calculate overlays and patches to know what's in there. For an example, see {ref}`use_layers_as_logical_groups`.
- While declarative is generally desirable, layered configurations can also accommodate imperative overrides when necessary. For example, we can define base layers, environment-specific override layers, and temporary patch layers, so that we can apply the base layers across all environments, apply a temporary patch to a specific service, or customize a service for a particular environment. The way to achieve this in Pebble is the same as how to organize services into logical groups, with each environment-specific setup as overlays. For an example, see {ref}`use_override_to_configure_environments_differently`.

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

For full details of all fields, see the [complete layer specification](../reference/layer-specification).

## Layer override

Each layer can define new services or modify existing ones defined in preceding layers. Crucially, the mandatory `override` field in each service definition determines how the layer's configuration interacts with the previously defined service of the same name (if any):

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

```yaml
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

(use_layers_as_logical_groups)=
## Use layers as logical groups

If we are to orchestrate multiple applications, we can group related ones into the same layer.

For example, if we have an Nginx webserver serving two applications, each with a frontend, a backend and a database:

```
nginx ──┬──> app1-frontend -> app1->backend -> app1-db
        └──> app2-frontend -> app2->backend -> app2-db
```

We can group them by application. For example:

`001-nginx.yaml`:

```yaml
summary: Nginx layer

description: |
    An Nginx layer.

services:
    nginx:
        override: replace
        summary: Nginx
        command: foo
        startup: enabled
```

`002-app1.yaml`:

```yaml
summary: Layer for app1

description: |
    Layer for app1.

services:
    app1-database:
        override: replace
        summary: database for app1
        command: foo
        startup: enabled
    app1-backend:
        override: replace
        summary: backend for app1
        command: foo
        after:
            - app1-database
    app1-frontend:
        override: replace
        summary: frontend for app1
        command: foo
        startup: enabled
        after:
            - app1-backend
```

`003-app2.yaml`:

```yaml
summary: Layer for app2

description: |
    Layer for app2.

services:
    app2-database:
        override: replace
        summary: database for app2
        command: foo
        startup: enabled
    app2-backend:
        override: replace
        summary: backend for app2
        command: foo
        after:
            - app2-database
    app2-frontend:
        override: replace
        summary: frontend for app2
        command: foo
        startup: enabled
        after:
            - app2-backend
```

Alternatively, we can also group them by functionality, and it will yield the same result:

`001-webserver.yaml`:

```yaml
summary: Nginx layer

description: |
    An Nginx layer.

services:
    nginx:
        override: replace
        summary: Nginx
        command: foo
        startup: enabled
```

`002-database.yaml`:

```yaml
summary: Layer for database

description: |
    Layer for database.

services:
    app1-database:
        override: replace
        summary: database for app1
        command: foo
        startup: enabled
    app2-database:
        override: replace
        summary: database for app2
        command: foo
        startup: enabled
```

`003-backend.yaml`:

```yaml
summary: Layer for backend

description: |
    Layer for backend.

services:
    app1-backend:
        override: replace
        summary: backend for app1
        command: foo
        startup: enabled
    app2-backend:
        override: replace
        summary: backend for app2
        command: foo
        startup: enabled
```

`004-frontend.yaml`:

```yaml
summary: Layer for frontend

description: |
    Layer for frontend.

services:
    app1-frontend:
        override: replace
        summary: frontend for app1
        command: foo
        startup: enabled
    app2-frontend:
        override: replace
        summary: frontend for app2
        command: foo
        startup: enabled
```

(use_override_to_configure_environments_differently)=
## Use override to configure environments differently

If we are to run the same set of services in multiple environments with slightly different configurations, we can use a base layer/environment override structure.

For example, if we have a service `app1` running in both `dev` and `test` environments with slightly different configurations (for example, an environment variable named `ENV` with values as `dev` or `test`), we can define a base layer and an override layer for each environment.

`001-base-layer.yaml`:

```yaml
summary: Simple layer

description: |
    A better description for a simple layer.

services:
    app1:
        override: replace
        summary: Service summary
        command: cmd arg1 "arg2a arg2b"
        startup: enabled
```

`002-env-override-dev.yaml`:

```yaml
summary: Override layer for the dev environment.

services:
    app1:
        override: merge
        environment:
            ENV: dev
```

`002-env-override-test.yaml`:

```yaml
summary: Override layer for the test environment.

services:
    app1:
        override: merge
        environment:
            ENV: test
```

In this way, when we use Pebble to run this service in different environments, we can put `001-base-layer.yaml` into all environments. Then we can put `002-env-override-dev.yaml` in the dev environment only, and `003-env-override-test.yaml` in the test environment only.

Dev env:

```bash
.
└── layers
    ├── 001-base-layer.yaml
    └── 002-env-override-dev.yaml
```

Test env:

```bash
.
└── layers
    ├── 001-base-layer.yaml
    └── 002-env-override-test.yaml
```

## See more

- [Layer specification](/reference/layer-specification.md)
