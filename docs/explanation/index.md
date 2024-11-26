# Explanation

These guides explain how Pebble works.


## Fundamentals

Pebble works as a daemon, with state and configuration stored in the `$PEBBLE` directory.

```{toctree}
:titlesonly:
:maxdepth: 1

General model <general-model>
```


## Access to the daemon

The daemon exposes an API that remote clients can connect to. By default, Pebble restricts access to some API endpoints based on the user that is connecting. You can set up named identities to grant specific access levels to users.

```{toctree}
:titlesonly:
:maxdepth: 1

API and clients <api-and-clients>
```


## Service orchestration

Pebble can automatically start and stop services according to dependencies in your service definition.

```{toctree}
:titlesonly:
:maxdepth: 1

Service dependencies <service-dependencies>
Service start order <service-start-order>
```