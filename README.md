## Take control of your internal daemons!

**Pebble** helps you to orchestrate a set of local service processes as an organized set.
It resembles well known tools such as _supervisord_, _runit_, or _s6_, in that it can
easily manage non-system processes independently from the system services, but it was
designed with unique features that help with more specific use cases.

- [General model](#general-model)
- [Layer configuration examples](#layer-configuration-examples)
- [Running pebble](#running-pebble)
- [Layer specification](#layer-specification)
- [TODO/Contributing](#todo-contributing)

## General model

Pebble is organized as a single binary that works as a daemon and also as a
client to itself. When the daemon runs it loads its own configuration from the
`$PEBBLE` directory, as defined in the environment, and also writes down in
that same directory its state and Unix sockets for communication. If that variable
is not defined, Pebble will attempt to look for its configuration from a default
system-level setup at `/var/lib/pebble/default`. Using that directory is encouraged
for whole-system setup such as when using Pebble to control services in a container.

The `$PEBBLE` directory must contain a `layers/` subdirectory that holds a stack of
configuration files with names similar to `001-base-layer.yaml`, where the digits define
the order of the layer and the following label uniquely identifies it. Each
layer in the stack sits above the former one, and has the chance to improve or
redefine the service configuration as desired.

## Layer configuration examples

This is a complete example of the current configuration format. You can also see the [layer specification](#layer-specification):

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

Some details worth highlighting:

- The `startup` option can be `enabled` or `disabled`.
- There is the `override` field (for now required) which defines whether this
  entry _overrides_ the previous service of the same name (if any - missing is
  okay), or merges with it.

### Layer override example

Any of the fields can be replaced individually in a merged service configuration.
To illustrate, here is a sample override layer that might sit atop the one above:

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

## Running pebble

Once the `$PEBBLE` directory is setup, running it is easy:

    $ pebble run

This will start the pebble daemon itself, and start all default services as well. Then
other pebble commands may be used to interact with the running daemon.

For example, to see any recent changes, for this or previous runs, use:

    $ pebble changes

And start or stop a specific service with:

    $ pebble start <name1> [<name2> ...]
    $ pebble stop  <name1> [<name2> ...]

## Layer specification

### Type: Layer

| Field         |         Type         | Default | Required? | Description                                                                                     |
| :------------ | :------------------: | :-----: | :-------: | :---------------------------------------------------------------------------------------------- |
| `summary`     |       `string`       |  `""`   |    no     | Short one line summary of the layer                                                             |
| `description` |       `string`       |  `""`   |    no     | A full description of the layer                                                                 |
| `services`    | `map[string]Service` |  `[]`   |    yes    | A map of services for Pebble to manage. The key represents the name of the service in the layer |

### Type: Service

| Field         |                       Type                        | Default | Required? | Description                                                                                                                              |
| :------------ | :-----------------------------------------------: | :-----: | :-------: | :--------------------------------------------------------------------------------------------------------------------------------------- |
| `summary`     |                     `string`                      |  `""`   |    no     | Short one line summary of the service.                                                                                                   |
| `description` |                     `string`                      |  `""`   |    no     | A full description of the service.                                                                                                       |
| `startup`     |  [`ServiceStartup`](#enumeration-servicestartup)  |  `""`   |    no     | Value `enabled` will ensure service is started automatically when Pebble starts. Value `disabled` will do the opposite.                  |
| `override`    | [`ServiceOverride`](#enumeration-serviceoverride) |  `""`   |    yes    | Control the effect the new layer will have on the current Pebble plan.                                                                   |
| `command`     |                     `string`                      |  `""`   |    yes    | Command for Pebble to run. Example `/usr/bin/myapp -a -p 8080`.                                                                          |
| `after`       |                    `[]string`                     |  `""`   |    no     | Array of other service names in the plan that should start before the service.                                                           |
| `before`      |                    `[]string`                     |  `""`   |    no     | Array of other service names in the plan that this service should start before.                                                          |
| `requires`    |                    `[]string`                     |  `""`   |    no     | Array of other service names in the plan that this service requires to be started before being started itself.                           |
| `environment` |                `map[string]string`                |  `""`   |    no     | A map of environment variable names, and their respective values, that should be injected into the environment of the service by Pebble. |

### Enumeration: ServiceStartup

| Value      | Description                                                |
| :--------- | :--------------------------------------------------------- |
| `enabled`  | Start the service automatically when Pebble starts.        |
| `disabled` | Do not start the service automatically when Pebble starts. |

### Enumeration: ServiceOverride

| Value      | Description                                                                                                                        |
| :--------- | :--------------------------------------------------------------------------------------------------------------------------------- |
| `merge`    | Merge the specification of the service with any existing specification of a service with the same name in the current Pebble plan. |
| `override` | Override any services of the same name in the current Pebble plan.                                                                 |

## TODO/Contributing

This is a preview of what Pebble is becoming. Please keep that in mind while you
explore around.

Here are some of the things coming soon:

- [x] Support `$PEBBLE_SOCKET` and default `$PEBBLE` to `/var/lib/pebble/default`
- [x] Define and enforce convention for layer names
- [x] Dynamic layer support over the API
- [x] Configuration retrieval commands to investigate current settings
- [x] Status command that displays active services and their current status
- [x] General system modification commands (writing configuration files, etc)
- [ ] Improve signal handling, e.g., sending SIGHUP to a service
- [ ] Terminate all services before exiting run command
- [ ] More tests for existing CLI commands
- [ ] Better log caching and retrieval support
- [ ] Consider showing unified log as output of `pebble run`

## Have fun!

... and enjoy the rest of 2021!
