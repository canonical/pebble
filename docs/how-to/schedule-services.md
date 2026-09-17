# How to run services on a schedule

It can be useful to start a service periodically, either to ensure that a service
is no longer stopped or to perform some action.

For example, if you have a service that handles software updates, an administrator
might manually stop this service while they perform maintainence on a system. If
the administrator forgets to start the service again, important software updates
might be missed. By setting a schedule to start the service, we can ensure it
is running again.

Another example that is often seen with systems using TLS certificates, is that
the certificates are issued with a short validity period requiring renewal before
they expire. Using a tool like Certbot, we can fetch new certificates, but for
this to help, we need Certbot to run before the certificate expires.

(run-a-service-on-a-schedule)=
## Run a service on a schedule

To ensure a service is started, a schedule can be used to start the service.
This is useful to either delay the start of a service or to ensure the service
is started if it was manually stopped.

This is an example configuration for a scheduled start service that ensures the
service is running once per day:

```yaml
services:
  svc1:
    override: replace
    command: daily.sh
    startup: disabled
    schedule: 0:00 # Once per day at 0:00.
```

(run-a-service-as-a-cron-job)=
## Run a service as a cron job

To run a service as a scheduled repeating one-shot service, it is required to
set the `on-success` field to ignore when the service process exits.
Since the schedule will eventually start the service again, it may be useful to
set the `on-failure` field to ignore the failure.

This is an example configuration for a cron job service:

```yaml
services:
  svc1:
    override: replace
    command: ping -c 3 localhost
    startup: disabled
    schedule: 0:00-24:00/1440 # Once per minute.
    on-success: ignore
    on-failure: ignore
```

## See more

- [Layer specification](../reference/layer-specification)
- [Timer string format](../reference/timer-string-format)
