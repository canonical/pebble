# Reference

These guides provide technical information about Pebble.


## Layers

Your service configuration is defined as a stack of "layers".

```{toctree}
:titlesonly:
:maxdepth: 1

Layers <layers>
Layer specification <layer-specification>
```


## Pebble commands

The `pebble` command has several subcommands.

```{toctree}
:titlesonly:
:maxdepth: 1

CLI Commands <cli-commands/cli-commands>
```


## Pebble in containers

When Pebble is configured as a client connected to a remote system (e.g., a separate container), you can use subcommands on the client to manage the remote system.

```{toctree}
:titlesonly:
:maxdepth: 1

Use Pebble in containers <pebble-in-containers>
```


## Access to the API

You can set up named "identities" to control access to the API.

```{toctree}
:titlesonly:
:maxdepth: 1

Identities <identities>
```


## Service failures

Pebble provides two ways to automatically restart services when they fail. Auto-restart is based on exit codes from services. Health checks are a more sophisticated way to test and report the availability of services.

```{toctree}
:titlesonly:
:maxdepth: 1

Service auto-restart <service-auto-restart>
Health checks <health-checks>
```


## Changes and tasks

Pebble tracks system changes as "tasks" grouped into "change" objects.

```{toctree}
:titlesonly:
:maxdepth: 1

Changes and tasks <changes-and-tasks>
```


## Notices

Pebble records events as "notices". In addition to the built-in notices, clients can report custom notices.

```{toctree}
:titlesonly:
:maxdepth: 1

Notices <notices>
```


## Log forwarding

Pebble can send service logs to a Loki server.

```{toctree}
:titlesonly:
:maxdepth: 1

Log forwarding <log-forwarding>
```
