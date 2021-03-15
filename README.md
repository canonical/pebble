
## Take control of your internal daemons!

**Pebble** helps you to orchestrate a set of local service processes as an organized set.
It resembles well known tools such as _supervisord_, _runit_, or _s6_, in that it can
easily manage non-system processes independently from the system services, but it was
designed with unique features that help with more specific use cases.

## General model

Pebble is organized as a single binary that works as a daemon and also as a
client to itself. When the daemon runs it loads its own configuration from the
`$PEBBLE` directory, as defined in the environment, and also writes down in
that same directory its state and unix sockets for communication. If that variable
is not defined, Pebble will attempt to look for its configuration from a default
system-level setup at `/var/lib/pebble/default`. Using that directory is encouraged
for whole-system setup such as when using Pebble to control services in a container.

The `$PEBBLE` directory must contain a `layers/` subdirectory that holds a stack of
configuration files with names similar to `001-base-layer.yaml`, where the digits define
the order of the layer and the following label uniquely identifies it.  Each
layer in the stack sits above the former one, and has the chance to improve or
redefine the service configuration as desired.

## Layer configuration

This is a complete example of the current configuration format:

```yaml
summary: Simple layer

description: |
    A better description for a simple layer.

services:

    srv1:
        override: replace
        summary: Service summary
        command: cmd arg1 "arg2a arg2b"
        default: start
        after:
            - srv2
        before:
            - srv3
        requires:
            - srv2
            - srv3
        environment:
            - VAR1: val1
            - VAR2: val2
            - VAR3: val3

    srv2:
        override: replace
        default: start
        command: cmd
        before:
            - srv3

    srv3:
        override: replace
        command: cmd
```

The file should be almost entirely obvious. One interesting detail there is the `override`
field (for now required) which defines whether this entry _overrides_ the previous
service of the same name (if any - missing is okay), or merges with it. Any of the fields can
be replaced individually in a merged service configuration.

To illustrate, here is a sample override layer that might sit atop the one above:

```yaml
summary: Simple override layer

services:

    srv1:
        override: merge
        environment:
            - VAR3: val3
        after:
            - srv4
        before:
            - srv5

    srv2:
        override: replace
        summary: Replaced service
        default: stop
        command: cmd

    srv4:
        override: replace
        command: cmd
        default: start

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


## XXX THIS IS EXPERIMENTAL XXX

This is a preview of what Pebble is becoming. Please keep that in mind while you
explore around.

Here are some of the things coming soon:

  - [x] Support `$PEBBLE_SOCKET` and default `$PEBBLE` to `/var/lib/pebble/default`
  - [x] Define and enforce convention for layer names
  - [x] Dynamic layer support over the API
  - [x] Configuration retrieval commands to investigate current settings
  - [x] Status command that displays active services and their current status
  - [ ] General system modification commands (writing configuration files, etc)
  - [ ] Improve signal handling, e.g., sending SIGHUP to a service
  - [ ] Terminate all services before exiting run command
  - [ ] More tests for existing CLI commands
  - [ ] Better log caching and retrieval support
  - [ ] Consider showing unified log as output of `pebble run`

## API

The documentation for the currently exposed API endpoints can be found in `doc/api.yaml`
In that folder you can also find a nice looking html file reflecting the contents of the file above.
The html file can be generated with: `npx redoc-cli bundle doc/api.yaml --output doc/index.html`

## Have fun!

... and enjoy the end of 2020!

