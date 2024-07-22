# How to manage service dependencies

If you are using Pebble to manage your services, chances are, you've got more than one service to manage.

And when orchestrating more services, things can and will get trickier, because more often than not, those services depend on each other to function together.

This document shows how to manage a set of services that have dependencies on each other to function properly.

## A simple web application

To demonstrate the instructions in this guide, we are using a simple web application with the following setup:

- a database listening on port 3306
- a backend server listening on port 8081, which talks to the database
- a frontend server listening on port 8080, which talks to the backend

The relationship between the frontend server, backend server and database can be simplified in the form of a simple diagram:

`frontend (8080) -> backend (8081) -> database (3306)`

These components (or services) are all dependent on one another; if any component fails to start or starts with errors, the web application won't function properly.

## Layers configuration

For the above example, we may have Pebble layers configured as follows:

```yaml
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
We use Python's http module to mock the servers.
```

If we start the services by running:

```{terminal}
   :input: pebble run
2024-06-28T02:17:23.347Z [pebble] Started daemon.
[backend database frontend]
2024-06-28T02:17:23.353Z [pebble] POST /v1/services 2.613042ms 202
2024-06-28T02:17:23.356Z [pebble] Service "backend" starting: python3 -m http.server 8081
2024-06-28T02:17:24.363Z [pebble] Service "database" starting: python3 -m http.server 3306
2024-06-28T02:17:25.370Z [pebble] Service "frontend" starting: python3 -m http.server 8080
2024-06-28T02:17:26.385Z [pebble] Started default services with change 1.
```

We can see they are all started, which can be verified by running this in another terminal:

```{terminal}
   :input: pebble services
Service   Startup  Current  Since
backend   enabled  active   today at 10:17 CST
database  enabled  active   today at 10:17 CST
frontend  enabled  active   today at 10:17 CST
```

## Problems

The layer configuration shown above works, but it's not exactly ideal:

- In this example, the three components rely on each other. Without any one of them, our whole web app won't function properly.
- If the database isn't started (or can't be started for some reason), it doesn't make sense to start the backend that depends on it. Even if the backend started, it would fail to communicate with the database, so the whole web app won't function properly.
- The same is true for the frontend: if the backend isn't started (if it fails to start or can't be started), it makes no sense to start the frontend alone.

The following example shows the database failing to start, but the database and frontend starting successfully:

```{terminal}
   :input: pebble run
2024-06-28T02:20:03.337Z [pebble] Started daemon.
[backend database frontend]
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

As we can see, although the `database` is `inactive`, the backend and frontend are both `active`. This isn't exactly useful, since all they are doing is consuming resources without being able to provide any service.

Wouldn't it be nice to tell Pebble that these services depend on each other? If a dependency failed to start, Pebble could put the services that rely on it "on hold" instead of starting them. This is where service dependencies come in.

## Service dependencies

Pebble can take service dependencies into account when managing services: this is done with the `required` list in the [service definition](../reference/layer-specification.md).

Simply put, you can configure a list of other services in the `requires` section to indicate this service requires those other services to start correctly.

When Pebble starts a service, it also starts the services which that service depends on (configured with `requires`). Conversely, when stopping a service, Pebble also stops services which depend on that service.

For example, if service `nginx` requires `logger`, `pebble start nginx` will start both `nginx` and `logger` (in an undefined order). Running `pebble stop logger` will stop both `nginx` and `logger`; however, running `pebble stop nginx` will only stop `nginx` (`nginx` depends on `logger`, not the other way around).

## Start order

When multiple services need to be started together, they're started in order according to the `before` and `after` in the [layer configuration](../reference/layer-specification.md). Pebble waits 1 second after starting each service to ensure the command doesn't exit too quickly.

The `before` option is a list of services that this service must start before (it may or may not `require` them). Or if it's easier to specify this ordering the other way around, `after` is a list of services that this service must start after.

```{note}
Currently, `before` and `after` are of limited usefulness, because Pebble only waits 1 second before moving on to start the next service, with no additional checks that the previous service is operating correctly.

If the configuration of `requires`, `before`, and `after` for a service results in a cycle or "loop", an error will be returned when attempting to start or stop the service.
```

## Putting it together

With the service dependencies and start order features, let's reconfigure our web app layer:

`001-my-web-app-layer.yaml`:

```yaml
services:
  database:
    override: replace
    command: python3 -m http.server 3306
    startup: enabled
  backend:
    override: replace
    command: python3 -m http.server 8081
    startup: enabled
    requires:
      - database
    after:
      - database
  frontend:
    override: replace
    command: python3 -m http.server 8080
    startup: enabled
    requires:
      - backend
    after:
      - backend
```

If we try to start it now:

```{terminal}
   :input: pebble run
2024-06-28T03:53:23.307Z [pebble] Started daemon.
[database backend frontend]
2024-06-28T03:53:23.313Z [pebble] POST /v1/services 3.103291ms 202
2024-06-28T03:53:23.317Z [pebble] Service "database" starting: python3 -m http.server 3306
2024-06-28T03:53:24.324Z [pebble] Service "backend" starting: python3 -m http.server 8081
2024-06-28T03:53:25.332Z [pebble] Service "frontend" starting: python3 -m http.server 8080
2024-06-28T03:53:26.337Z [pebble] GET /v1/changes/1/wait 3.023563626s 200
2024-06-28T03:53:26.338Z [pebble] Started default services with change 1.
```

You can see how `database` is started first, then `backend`, then `frontend` -- this is due to the `after` ordering. And we can verify the status of the services:

```{terminal}
   :input: pebble services
Service   Startup  Current  Since
backend   enabled  active   today at 10:17 CST
database  enabled  active   today at 10:17 CST
frontend  enabled  active   today at 10:17 CST
```

There seems to be no difference when everything is working. But when one service fails to start, we can see the difference. For example, here's what happens if the database can't be started (say, the port is already taken):

```{terminal}
   :input: pebble run
2024-06-28T02:28:06.569Z [pebble] Started daemon.
[database backend frontend]
2024-06-28T02:28:06.575Z [pebble] POST /v1/services 3.212375ms 202
2024-06-28T02:28:06.578Z [pebble] Service "database" starting: python3 -m http.server 3306
2024-06-28T02:28:06.627Z [pebble] Change 1 task (Start service "database") failed: cannot start service: exited quickly with code 1
2024-06-28T02:28:06.633Z [pebble] GET /v1/changes/1/wait 57.610375ms 200
2024-06-28T02:28:06.633Z [pebble] Started default services with change 1.
```

If we check the state of the services:

```{terminal}
   :input: pebble services
Service   Startup  Current   Since
backend   enabled  inactive  -
database  enabled  inactive  -
frontend  enabled  inactive  -
```

We can see that since the database failed to start, the backend and the frontend are not started. We can further verify this using [changes and tasks](../reference/changes-and-tasks/changes-and-tasks.md):

```{terminal}
   :input: pebble changes
ID   Status  Spawn               Ready               Summary
1    Error   today at 10:28 CST  today at 10:28 CST  Autostart service "database" and 2 more
```

```{terminal}
   :input: pebble tasks 1
Status  Spawn               Ready               Summary
Error   today at 10:28 CST  today at 10:28 CST  Start service "database"
Hold    today at 10:28 CST  today at 10:28 CST  Start service "backend"
Hold    today at 10:28 CST  today at 10:28 CST  Start service "frontend"

......................................................................
Start service "database"

2024-06-28T10:28:06+08:00 INFO Most recent service output:
    Traceback (most recent call last):
      File "/usr/lib/python3.10/runpy.py", line 196, in _run_module_as_main
        return _run_code(code, main_globals, None,
      File "/usr/lib/python3.10/runpy.py", line 86, in _run_code
        exec(code, run_globals)
      File "/usr/lib/python3.10/http/server.py", line 1307, in <module>
        test(
      File "/usr/lib/python3.10/http/server.py", line 1258, in test
        with ServerClass(addr, HandlerClass) as httpd:
      File "/usr/lib/python3.10/socketserver.py", line 452, in __init__
        self.server_bind()
      File "/usr/lib/python3.10/http/server.py", line 1301, in server_bind
        return super().server_bind()
      File "/usr/lib/python3.10/http/server.py", line 137, in server_bind
        socketserver.TCPServer.server_bind(self)
      File "/usr/lib/python3.10/socketserver.py", line 466, in server_bind
        self.socket.bind(self.server_address)
    OSError: [Errno 98] Address already in use
2024-06-28T10:28:06+08:00 ERROR cannot start service: exited quickly with code 1
```

We can see that since the database failed to start, other services that depend on it are not started (their service status remains "inactive").

## See more

- [pebble services command](../reference/pebble-services.md)
- [pebble start command](../reference/pebble-start.md)
- [pebble stop command](../reference/pebble-stop.md)
- [Changes and tasks](../reference/changes-and-tasks/changes-and-tasks.md)
- [Layer specification](../reference/layer-specification.md)
