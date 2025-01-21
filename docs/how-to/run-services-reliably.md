# How to run services reliably

Microservice architectures offer flexibility, but they can introduce reliability challenges such as network interruptions, resource exhaustion, problems with dependent services, cascading failures, and deployment issues. Health checks can address these issues by monitoring resource usage, checking the availability of dependencies, catching problems of new deployments, and preventing downtime by redirecting traffic away from failing services.

To help you manage services more reliably, Pebble provides a comprehensive health check feature.

## Use health checks of the HTTP type

A health check of the HTTP type issues HTTP `GET` requests to the health check URL at a user-specified interval.

The health check is considered successful if the check returns an HTTP 200 response. After getting a certain number of failures in a row, the health check is considered "down" (or unhealthy).

### Configure HTTP-type health checks

For example, we can configure a health check of HTTP type named `svc1-up` that checks the endpoint `http://127.0.0.1:5000/health`:

```yaml
checks:
  svc1-up:
    override: replace
    period: 10s
    timeout: 3s
    threshold: 3
    http:
      url: http://127.0.0.1:5000/health
```

The configuration above contains three key options that we can tweak for each health check:

- `period`: How often to run the check (defaults to 10 seconds).
- `timeout`: If the check hasn't responded before the timeout (defaults to 3 seconds), consider the check an error.
- `threshold`: After how many consecutive errors (defaults to 3) is the check considered "down".

Given the default values, a minimum check looks like the following:

```yaml
checks:
  svc1-up:
    override: replace
    http:
      url: http://127.0.0.1:5000/health
```

Besides the HTTP type, there are two more health check types in Pebble: `tcp`, which opens the given TCP port, and `exec`, which executes a user-specified command. For more information, see [Health checks](../reference/health-checks) and [Layer specification](../reference/layer-specification).

### Restart the service when the health check fails

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
- Health checks shouldn't be used for scheduling tasks like backups.

## See more

- [Health checks](../reference/health-checks)
- [Layer specification](../reference/layer-specification)
- [Service lifecycle](../reference/service-lifecycle)
