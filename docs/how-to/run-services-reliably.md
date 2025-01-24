# How to run services reliably

Microservice architectures offer flexibility, but they can introduce reliability challenges such as network interruptions, resource exhaustion, problems with dependent services, cascading failures, and deployment issues. Health checks can address these issues by monitoring resource usage, checking the availability of dependencies, catching problems with new deployments, and preventing downtime by redirecting traffic away from failing services.

To help you manage services more reliably, Pebble provides a health check feature.

## Use HTTP health checks

A health check of `http` type issues HTTP `GET` requests to the health check URL at a user-specified interval.

The health check is considered successful if the URL returns any HTTP 2xx response. After getting a certain number of errors in a row, the health check fails and is considered "down" (or "unhealthy").

For example, we can configure a health check of type `http` named `svc1-up` that checks the endpoint `http://127.0.0.1:5000/health`:

```yaml
checks:
  svc1-up:
    override: replace
    period: 5s    # default 10s
    timeout: 1s   # default 3s
    threshold: 5  # default 3
    http:
      url: http://127.0.0.1:5000/health
```

The configuration above contains three key options that we can tweak for each health check:

- `period`: How often to run the check.
- `timeout`: If the check hasn't responded before the timeout, consider the check an error.
- `threshold`: After this many consecutive errors, the check is considered "down".

If we're happy with the default values, a minimum check looks like the following:

```yaml
checks:
  svc1-up:
    override: replace
    http:
      url: http://127.0.0.1:5000/health
```

Besides the `http` type, there are two more health check types in Pebble: `tcp`, which opens the given TCP port, and `exec`, which executes a user-specified command. For more information, see [Health checks](../reference/health-checks) and [Layer specification](../reference/layer-specification).

## Restart a service when the health check fails

To automatically restart services when a health check fails, use `on-check-failure` in the service configuration.

To restart `svc1` when the health check named `svc1-up` fails, use the following configuration:

```yaml
services:
  svc1:
    override: replace
    command: python3 /home/ubuntu/work/health-check-sample-service/main.py
    startup: enabled
    on-check-failure:
      svc1-up: restart
```

## Limitations of health checks

Although health checks are useful, they are not a complete solution for reliability:

- Health checks can detect issues such as a failed database connection due to network issues, but they can't fix the network issue itself.
- Health checks also can't replace testing and monitoring.
- Health checks shouldn't be used for scheduling tasks such as backups. Use a cron-style tool for that.

## See more

- [Health checks](../reference/health-checks)
- [Layer specification](../reference/layer-specification)
- [Service auto-restart](../reference/service-auto-restart)
