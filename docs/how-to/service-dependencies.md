# How to manage service dependencies

If you are using Pebble to manage your services, chances are, you've got more
than one service to manage.

And when orchestrating more services, things can and will get trickier, because
more often than not, those services depend on each other to function together.

This document shows how to manage a set of services that have dependencies on
each other to function properly.

## Demo web application

We are using a simple web application to demonstrate the instructions in this
guide.

### Setup

The web application has the following setup:

- a database listening on port 3306
- a backend server listening on port 8081, which talks to the database
- a frontend server listening on port 8080, which talks to the backend

The relationship between the frontend server, backend server and database can be
simplified as:

`frontend (8080) -> backend (8081) -> database (3306)`

These components (or services) are all dependent on one another; if any
component fails to start or starts with errors, the web application won't
function properly.

For example, if the backend server fails to start, or is unable to
communicate with the database, the frontend service will not run successfully.

### Layer configuration

The web application is initially configured with the Pebble layer below:

```{code-block} yaml
   
services:
  frontend:
    override: replace
    command: python3 -m http.server 8080
    startup: enabled
  backend:
    override: replace
    command: python3 -m http.server 8081
    startup: enabled
  database:
    override: replace
    command: python3 -m http.server 3306
    startup: enabled
```

```{note}
We are using Python's http module to mock the servers.
```
### Problem with setup

If we start the Pebble daemon (`pebble run`):

```{terminal}
   :input: pebble run

2024-06-28T02:17:23.347Z [pebble] Started daemon.
2024-06-28T02:17:23.353Z [pebble] POST /v1/services 2.613042ms 202
2024-06-28T02:17:23.356Z [pebble] Service "backend" starting: python3 -m http.server 8081
2024-06-28T02:17:24.363Z [pebble] Service "database" starting: python3 -m http.server 3306
2024-06-28T02:17:25.370Z [pebble] Service "frontend" starting: python3 -m http.server 8080
2024-06-28T02:17:26.385Z [pebble] Started default services with change 1.
```

Ideally we would expect all three services to start up successfully
(`pebble services`):

```{terminal}
   :input: pebble services

Service   Startup  Current  Since
backend   enabled  active   today at 10:17 CST
database  enabled  active   today at 10:17 CST
frontend  enabled  active   today at 10:17 CST
```

However, this configuration does not account for the service dependencies.

For example, the output below shows the `database` service failing to start
("inactive"), but the `backend` and `frontend` services starting
successfully ("active"):

```{terminal}
   :input: pebble run

2024-06-28T02:20:03.337Z [pebble] Started daemon.
2024-06-28T02:20:03.343Z [pebble] POST /v1/services 2.763792ms 202
2024-06-28T02:20:03.346Z [pebble] Service "backend" starting: python3 -m http.server 8081
2024-06-28T02:20:03.346Z [pebble] Service "database" starting: python3 -m http.server 3306
2024-06-28T02:20:03.347Z [pebble] Service "frontend" starting: python3 -m http.server 8080
2024-06-28T02:20:03.396Z [pebble] Change 1 task (Start service "database") failed: cannot start service: exited quickly with code 1
2024-06-28T02:20:04.353Z [pebble] Started default services with change 1.
```

```{terminal}
   :input: pebble services
   
Service   Startup  Current   Since
backend   enabled  active    today at 10:20 CST
database  enabled  inactive  -
frontend  enabled  active    today at 10:20 CST
```

We can configure the layer so that Pebble only starts a given service if all
other services it is dependent on are running successfully to avoid consuming
resources unnecessarily.

## Define service dependencies

To create service dependencies in Pebble, use the `requires` key with
`before` / `after` in the
[service definition](../reference/layer-specification.md).

### Specify dependent services

To specify one or more services that a given service requires to run
successfully, use the `requires` key.

```{code-block} yaml
   :emphasize-lines: 6, 7, 12, 13

services:
  frontend:
    override: replace
    command: python3 -m http.server 8080
    startup: enabled
    requires:
      - backend
  backend:
    override: replace
    command: python3 -m http.server 8081
    startup: enabled
    requires:
      - database
  database:
    override: replace
    command: python3 -m http.server 3306
    startup: enabled
```

In the layer configuration above, the `frontend` service is dependent on the
`backend` service, and the `backend` service is dependent on the `database`
service.

For more information on `requires`, see [Service dependencies](../explanation/service-dependencies.md).

### Specify start order for dependent services

To specify the order in which one or more dependent services must start
successfully, relative to a given service, use the `before` or `after` keys.

```{code-block} yaml
   :emphasize-lines: 8, 9, 16, 17

services:
  frontend:
    override: replace
    command: python3 -m http.server 8080
    startup: enabled
    requires:
      - backend
    after:
      - backend
  backend:
    override: replace
    command: python3 -m http.server 8081
    startup: enabled
    requires:
      - database
    after:
      - database
  database:
    override: replace
    command: python3 -m http.server 3306
    startup: enabled
```

In the updated layer above, the `frontend` service requires the `backend`
service to be started before it, and the `backend` service requires the
`database` service to be started before it.

```{include} /reuse/service-start-order.md
   :start-after: Start: Service start order note
   :end-before: End: Service start order note
```

For more information on `before` and `after`, see [Service start order](../explanation/service-start-order.md).

## Verify service dependencies

To verify the start order of services, start the Pebble daemon again:

```{terminal}
   :input: pebble run

2024-06-28T03:53:23.307Z [pebble] Started daemon.
2024-06-28T03:53:23.313Z [pebble] POST /v1/services 3.103291ms 202
2024-06-28T03:53:23.317Z [pebble] Service "database" starting: python3 -m http.server 3306
2024-06-28T03:53:24.324Z [pebble] Service "backend" starting: python3 -m http.server 8081
2024-06-28T03:53:25.332Z [pebble] Service "frontend" starting: python3 -m http.server 8080
2024-06-28T03:53:26.337Z [pebble] GET /v1/changes/1/wait 3.023563626s 200
2024-06-28T03:53:26.338Z [pebble] Started default services with change 1.
```

The start order is now `database` -> `backend` -> `frontend` and all services
are "active".

```{terminal}
   :input: pebble services
   
Service   Startup  Current  Since
backend   enabled  active   today at 10:17 CST
database  enabled  active   today at 10:17 CST
frontend  enabled  active   today at 10:17 CST
```

To further verify the service dependencies, we force a service that is required
by another service to fail.

In this example, if we force the `database` service to fail (by using port 3306
for another process), the output for the `pebble run` command should be similar
to:

```{terminal}
   :input: pebble run 

2024-06-28T02:28:06.569Z [pebble] Started daemon.
2024-06-28T02:28:06.575Z [pebble] POST /v1/services 3.212375ms 202
2024-06-28T02:28:06.578Z [pebble] Service "database" starting: python3 -m http.server 3306
2024-06-28T02:28:06.627Z [pebble] Change 1 task (Start service "database") failed: cannot start service: exited quickly with code 1
2024-06-28T02:28:06.633Z [pebble] GET /v1/changes/1/wait 57.610375ms 200
2024-06-28T02:28:06.633Z [pebble] Started default services with change 1.
```

Since a required service fails to start, all services that are dependent on it
should not start ("inactive") accordingly:

```{terminal}
   :input: pebble services

Service   Startup  Current   Since
backend   enabled  inactive  -
database  enabled  inactive  -
frontend  enabled  inactive  -
```

You can use the [Changes and tasks] commands to get more details about the
failed run.

## See more

- [pebble services command](../reference/cli-commands/services.md)
- [pebble start command](../reference/cli-commands/start.md)
- [pebble stop command](../reference/cli-commands/stop.md)
- [Layer specification](../reference/layer-specification.md)
- [Changes and tasks](/reference/changes-and-tasks.md).
