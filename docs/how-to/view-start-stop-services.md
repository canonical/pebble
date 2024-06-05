# How to view, start, and stop services

You can view the status of one or more services by using `pebble services`:

```
$ pebble services srv1       # show status of a single service
Service  Startup  Current
srv1     enabled  active

$ pebble services            # show status of all services
Service  Startup   Current
srv1     enabled   active
srv2     disabled  inactive
```

The "Startup" column shows whether this service is automatically started when Pebble starts ("enabled" means auto-start, "disabled" means don't auto-start).

The "Current" column shows the current status of the service, and can be one of the following:

* `active`: starting or running
* `inactive`: not yet started, being stopped, or stopped
* `backoff`: in a [backoff-restart loop](#service-auto-restart)
* `error`: in an error state

To start specific services, type `pebble start` followed by one or more service names:

```
$ pebble start srv1 srv2  # start two services (and any dependencies)
```

When starting a service, Pebble executes the service's `command`, and waits 1 second to ensure the command doesn't exit too quickly. Assuming the command doesn't exit within that time window, the start is considered successful, otherwise `pebble start` will exit with an error, regardless of the `on-failure` value.

Similarly, to stop specific services, use `pebble stop` followed by one or more service names:

```
$ pebble stop srv1        # stop one service
```

When stopping a service, Pebble sends SIGTERM to the service's process group, and waits up to 5 seconds. If the command hasn't exited within that time window, Pebble sends SIGKILL to the service's process group and waits up to 5 more seconds. If the command exits within that 10-second time window, the stop is considered successful, otherwise `pebble stop` will exit with an error, regardless of the `on-failure` value.
