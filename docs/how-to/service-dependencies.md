# How to manage service dependencies

If you are using Pebble to manage your services, chances are, you've got more than one service to manage.

And when orchestrating more services, things can and will get trickier, because more often than not, those services depend on each other to function together, and managing dependencies is no mean feat.

Let's have a look at a concrete example of service dependencies.

## My web app

Imagine we are running a web application, which has:

- a database listening on port 3306
- a backend server listening on port 8081, which talks to the database
- a frontend server listening on port 8080, which talks to the backend

I.E.:

`frontend (8080) -> backend (8081) -> db (3306)`

## Layers configuration

For the above example, we may have Pebble layers configured as follows:

`001-frontend.yaml`:

```yaml
services:
  frontend-server:
    override: replace
    summary: frontend
    command: python3 -m http.server 8080
    startup: enabled
```

`002-backend.yaml`:

```yaml
services:
  backend-server:
    override: replace
    summary: backend
    command: python3 -m http.server 8081
    startup: enabled
```

`003-database.yaml`:

```yaml
services:
  database:
    override: replace
    summary: database
    command: python3 -m http.server 3306
    startup: enabled
```


```{note}
We use Python's http module to mock the servers.

Alternatively, you may choose to define those services in a single layer. For example:

`001-my-webapp-layer.yaml`:

```yaml
services:
  frontend-server:
    override: replace
    summary: frontend
    command: python3 -m http.server 8080
    startup: enabled
  backend-server:
    override: replace
    summary: backend
    command: python3 -m http.server 8081
    startup: enabled
  database:
    override: replace
    summary: database
    command: python3 -m http.server 3306
    startup: enabled
```

If we start the services by running:

```{terminal}
   :input: pebble run
2024-06-28T02:17:23.347Z [pebble] Started daemon.
[backend-server database frontend-server]
2024-06-28T02:17:23.353Z [pebble] POST /v1/services 2.613042ms 202
2024-06-28T02:17:23.356Z [pebble] Service "backend-server" starting: python3 -m http.server 8081
2024-06-28T02:17:24.363Z [pebble] Service "database" starting: python3 -m http.server 3306
2024-06-28T02:17:25.370Z [pebble] Service "frontend-server" starting: python3 -m http.server 8080
2024-06-28T02:17:26.385Z [pebble] Started default services with change 1.
```

We can see they are all started, which can be verified by:

```{terminal}
   :input: pebble services
Service          Startup  Current  Since
backend-server   enabled  active   today at 10:17 CST
database         enabled  active   today at 10:17 CST
frontend-server  enabled  active   today at 10:17 CST
```

## Problems

Apparently, the above layer configuration works but is not exactly ideal, because:

- In this example, the three components kind of rely on each other, without anyone, our whole web app won't function properly.
- If the database isn't started (or can't be started for some reason), it doesn't make sense to start the backend that depends on it. Even if the backend starts, it fails to communicate with the database, so the whole web app won't function properly.
- The same is true for the frontend: if the backend isn't started (fails to start, can't be started, etc.), it makes no sense to start the frontend alone.

For example, when the DB fails to start, other services are still up and running:

```{terminal}
   :input: pebble run
2024-06-28T02:20:03.337Z [pebble] Started daemon.
[backend-server database frontend-server]
2024-06-28T02:20:03.343Z [pebble] POST /v1/services 2.763792ms 202
2024-06-28T02:20:03.346Z [pebble] Service "backend-server" starting: python3 -m http.server 8081
2024-06-28T02:20:03.346Z [pebble] Service "database" starting: python3 -m http.server 3306
2024-06-28T02:20:03.347Z [pebble] Service "frontend-server" starting: python3 -m http.server 8080
2024-06-28T02:20:03.396Z [pebble] Change 1 task (Start service "database") failed: cannot start service: exited quickly with code 1
2024-06-28T02:20:04.353Z [pebble] Started default services with change 1.
```

```{terminal}
   :input: pebble services
Service          Startup  Current   Since
backend-server   enabled  active    today at 10:20 CST
database         enabled  inactive  -
frontend-server  enabled  active    today at 10:20 CST
```

As we can see, although `database` is in `inactive` state, the backend/frontend are still both started, which isn't exactly useful since all they are doing is consuming resources without being able to provide any service.

Wouldn't it be nice to make these services depend on each other, and when a dependency fails to start, put other services that rely on it on hold, instead of starting them anyway?

Worry no more, service dependency comes to the rescue.

## Service dependencies

Pebble can take service dependencies into account when managing services: the magical `required` (see more detail in [Layer specification](../reference/layer-specification.md)) feature.

Simply put, you can configure a list of other services in the `required` section to indicate this service requires those other services to start correctly.

When Pebble starts a service, it also starts the services which that service depends on (configured with `required`). Conversely, when stopping a service, Pebble also stops services which depend on that service.

For example, if service `nginx` requires `logger`, `pebble start nginx` will start both `nginx` and `logger` (in an undefined order). Running `pebble stop logger` will stop both `nginx` and `logger`; however, running `pebble stop nginx` will only stop `nginx` (`nginx` depends on `logger`, not the other way around).

## Start order

When multiple services need to be started together, they're started in order according to the `before` and `after` configuration (see more detail in [Layer specification](../reference/layer-specification.md)), waiting for 1 second for each to ensure the command doesn't exit too quickly.

The `before` option is a list of services that this service must start before (it may or may not `require` them). Or if it's easier to specify this ordering the other way around, `after` is a list of services that this service must start after.

```{note}
Currently, `before` and `after` are of limited usefulness, because Pebble only waits 1 second before moving on to start the next service, with no additional checks that the previous service is operating correctly.

If the configuration of `requires`, `before`, and `after` for a service results in a cycle or "loop", an error will be returned when attempting to start or stop the service.
```

## Putting it together

With the service dependencies and start order features, let's reconfigure our web app layer to:

`001-my-web-app-layer.yaml`:

```yaml
services:
  database:
    override: replace
    summary: database
    command: python3 -m http.server 3306
    startup: enabled
  backend-server:
    override: replace
    summary: backend
    command: python3 -m http.server 8081
    startup: enabled
    requires:
      - database
    after:
      - database
  frontend-server:
    override: replace
    summary: frontend
    command: python3 -m http.server 8080
    startup: enabled
    requires:
      - backend-server
    after:
      - backend-server
```

If we try to start it now:

```{terminal}
   :input: pebble run
2024-06-28T02:17:23.347Z [pebble] Started daemon.
[backend-server database frontend-server]
2024-06-28T02:17:23.353Z [pebble] POST /v1/services 2.613042ms 202
2024-06-28T02:17:23.356Z [pebble] Service "backend-server" starting: python3 -m http.server 8081
2024-06-28T02:17:24.363Z [pebble] Service "database" starting: python3 -m http.server 3306
2024-06-28T02:17:25.370Z [pebble] Service "frontend-server" starting: python3 -m http.server 8080
2024-06-28T02:17:26.385Z [pebble] Started default services with change 1.
```

And we can verify the status of the services:

```{terminal}
   :input: pebble services
Service          Startup  Current  Since
backend-server   enabled  active   today at 10:17 CST
database         enabled  active   today at 10:17 CST
frontend-server  enabled  active   today at 10:17 CST
```

OK, there seems no difference when everything is working. But when one service fails to start, we can see the difference - for example, when the DB can't be started (say, the port is already taken):

```{terminal}
   :input: pebble run
2024-06-28T02:28:06.569Z [pebble] Started daemon.
[database backend-server frontend-server]
2024-06-28T02:28:06.575Z [pebble] POST /v1/services 3.212375ms 202
2024-06-28T02:28:06.578Z [pebble] Service "database" starting: python3 -m http.server 3306
2024-06-28T02:28:06.627Z [pebble] Change 1 task (Start service "database") failed: cannot start service: exited quickly with code 1
2024-06-28T02:28:06.633Z [pebble] GET /v1/changes/1/wait 57.610375ms 200
2024-06-28T02:28:06.633Z [pebble] Started default services with change 1.
```

If we check the state of the services:

```{terminal}
   :input: pebble services
Service          Startup  Current   Since
backend-server   enabled  inactive  -
database         enabled  inactive  -
frontend-server  enabled  inactive  -
```

We can see that since the database failed to start, the backend and the frontend are not started. We can further verify this in Pebble [changes and tasks](../reference/changes-and-tasks/changes-and-tasks.md):

Change:

```{terminal}
   :input: pebble changes
ID   Status  Spawn               Ready               Summary
1    Error   today at 10:28 CST  today at 10:28 CST  Autostart service "database" and 2 more
```

Task:

```{terminal}
   :input: pebble tasks 1
Status  Spawn               Ready               Summary
Error   today at 10:28 CST  today at 10:28 CST  Start service "database"
Hold    today at 10:28 CST  today at 10:28 CST  Start service "backend-server"
Hold    today at 10:28 CST  today at 10:28 CST  Start service "frontend-server"

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

We can see that since the database failed to start, other services that depend on it are put in "Hold" status.

## See more

- [Pebble services command](../reference/pebble-services.md)
- [Pebble start command](../reference/pebble-start.md)
- [Pebble stop command](../reference/pebble-stop.md)
- [Changes and tasks](../reference/changes-and-tasks/changes-and-tasks.md)
- [Layer specification](../reference/layer-specification.md)
