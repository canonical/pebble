# Reference

These guides provide technical information about Pebble.

% COMMENT: This toctree is for the navigation sidebar only
%          Use an alphabetical listing of pages in the toctree
%          For each page, make sure there's also a link in a section below

```{toctree}
:hidden:
:titlesonly:
:maxdepth: 1

API <api>
Changes and tasks <changes-and-tasks>
CLI commands <cli-commands>
Health checks <health-checks>
Identities <identities>
Layers <layers>
Layer specification <layer-specification>
Log forwarding <log-forwarding>
Notices <notices>
Pebble in containers <pebble-in-containers>
Service lifecycle <service-lifecycle>
```


% COMMENT: The first few pages are presented in a more logical reading order


## Layers

Pebble configuration is defined as a stack of "layers".

* [Layers](layer-specification)
* [Layer specification](layer-specification)


## Pebble commands

The `pebble` command has several subcommands.

* [CLI commands](cli-commands)


## Pebble in containers

When the Pebble daemon is running inside a remote system (for example, a separate container), you can manage the remote system using subcommands on the Pebble client.

* [Pebble in containers](pebble-in-containers)


## Service failures

Pebble provides two ways to automatically restart services when they fail. Auto-restart is based on exit codes from services. Health checks are a more sophisticated way to test and report the availability of services.

* [Service lifecycle](service-lifecycle)
* [Health checks](health-checks)


% COMMENT: After this point, match the alphabetical listing of pages


## Changes and tasks

Pebble tracks system changes as "tasks" grouped into "change" objects.

* [Changes and tasks](changes-and-tasks)


## Identities

You can set up named "identities" to control access to the API.

* [Identities](identities)


## Log forwarding

Pebble can send service logs to a centralized logging system.

* [Log forwarding](log-forwarding)


## Notices

Pebble records events as "notices". In addition to the built-in notices, clients can report custom notices.

* [Notices](notices)

## Accessing the API

Pebble exposes API over HTTP to allow remote clients to interact with the daemon.

* [API](api)
