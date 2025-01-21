# Explanation

These guides explain how Pebble works.


## Fundamentals

Pebble works as a daemon and a client, with state and configuration stored in the `$PEBBLE` directory.

```{toctree}
:titlesonly:
:maxdepth: 1

General model <general-model>
```


## Access to the API

The daemon exposes an API that clients can connect to. By default, Pebble restricts access to some API endpoints based on the user that is connecting. You can set up named "identities" to grant specific access levels to users.

```{toctree}
:titlesonly:
:maxdepth: 1

API and clients <api-and-clients>
```


## Service orchestration

Pebble automatically starts and stops services if you specify dependencies between services.

```{toctree}
:titlesonly:
:maxdepth: 1

Service dependencies <service-dependencies>
Service start order <service-start-order>
```


## Security

To use Pebble in a secure way, pay attention to API access levels, the Pebble directory, and how you install Pebble.

```{toctree}
:titlesonly:
:maxdepth: 1

Security <security>
```
