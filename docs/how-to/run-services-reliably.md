# How to run services reliably

In this guide, we will look at service reliability challenges in the modern world and how we can mitigate them with Pebble's advanced feature - [Health checks](../reference/health-checks).

## Service reliability in the modern microservice world

With the rise of the microservice architecture, reliability is becoming more and more important than ever. First, let's explore some of the causes of unreliability in microservice architectures:

- Network Issues: Microservices rely heavily on network communications. Intermittent network failures, latency spikes, and connection drops can disrupt service interactions and lead to failures.
- Resource Exhaustion: A single microservice consuming excessive resources (CPU, memory, disk I/O, and so on) can impact not only its performance and availability but also potentially affect other services depending on it.
- Dependency Failures: Microservices often depend on other components, like a database or other microservices. If a critical dependency becomes unavailable, the dependent service might also fail.
- Cascading Failures: A failure in one service can trigger failures in other dependent services, creating a cascading effect that can quickly bring down a large part of the system.
- Deployment Issues: Frequent deployments can benefit microservices if managed properly. However, it can also introduce instability if not. Errors during deployment, incorrect configurations, or incompatible versions can all cause reliability issues.
- Testing and Monitoring Gaps: Insufficient testing and monitoring can make it difficult to identify issues proactively, leading to unexpected failures and longer MTTR (mean time to repair).

## Health checks

To mitigate the reliability issues mentioned above, we need specific tooling, and health checks are one of them - a key mechanism and a critical part of the software development lifecycle (SDLC) in the DevOps culture for monitoring and detecting potential problems in the modern microservice architectures and especially in containerized environments.

By periodically running health checks, some of the reliability issues listed above can be mitigated:

### Detect resource exhaustion

Health checks can monitor resource usage (CPU, memory, disk space) within a microservice. For example, if resource consumption exceeds predefined thresholds, the health check can signal an unhealthy state, allowing for remediation, for example, scaling up or scaling out the service, restarting it, or issuing alerts.

### Identify dependent service failures

Health checks can verify the availability of critical dependencies. A service's health check can include checks to ensure it can connect to its database, message queues, or other required services.

### Catch deployment issues

Health checks can be incorporated into the deployment process. After a new version of a service is deployed, the deployment pipeline can monitor its health status. If the health check fails, the deployment can be rolled back to the previous state, preventing a faulty version from affecting end users.

### Mitigate cascading failures

By quickly identifying unhealthy services, health checks can help prevent cascading failures. For example, load balancers and service discovery mechanisms can use health check information to route traffic away from failing services, giving them time to recover.

### More on health checks

Note that a health check is no silver bullet, it can't solve all the reliability challenges posed by the microservice architecture. For example, while health checks can detect the consequence of network issues (e.g., inability to connect to a dependency), they can't fix the underlying network problem itself; and while health checks are a valuable part of a monitoring strategy, they can't replace comprehensive testing and monitoring.

Please also note that although health checks are running on a schedule, they should not be used to run scheduled jobs such as periodic backups.

In summary, health checks are a powerful tool for improving the reliability of microservices by enabling early detection of problems and making automated recovery possible.

## Using health checks of the HTTP type

A health check of the HTTP type issues HTTP `GET` requests to the health check URL at a user-specified interval.

The health check is considered successful if the check returns an HTTP 200 response. After getting a certain number of failures in a row, the health check is considered "down" (or unhealthy).

### Configuring HTTP-type health checks

Let's say we have a service `svc1` with a health check endpoint at `http://127.0.0.1:5000/health`. To configure a health check of HTTP type named `svc1-up` that accesses the health check endpoint at a 30-second interval with a timeout of 1 second and considers the check down if we get 3 failures in a row, we can use the following configuration:

```yaml
checks:
    svc1-up:
        override: replace
        period: 30s
        timeout: 1s
        threshold: 3
        http:
            url: http://127.0.0.1:5000/health
```

The configuration above contains three key options that you can tweak for each health check:

- `period`: How often to run the check (defaults to 10 seconds).
- `timeout`: If the check hasn't responded before the timeout (defaults to 3 seconds), consider the check an error
- `threshold`: After how many consecutive errors (defaults to 3) is the check considered "down"

Besides the HTTP type, there are two more health check types in Pebble: `tcp`, which opens the given TCP port, and `exec`, which executes a user-specified command. For more information, see [Health checks](../reference/health-checks) and [Layer specification](../reference/layer-specification).

### Restarting the service when the health check fails

To automatically restart services when a health check fails, use `on-check-failure` in the service configuration.

To restart `svc1` when the health check named `svc1-up` fails, use the following configuration:

```
services:
    svc1:
        override: replace
        command: python3 /home/ubuntu/work/health-check-sample-service/main.py
        startup: enabled
        on-check-failure:
            svc1-up: restart
```

## Demo service

To demonstrate Pebble health checks and auto-restart on health check failures, we created [a simple demo service](https://github.com/IronCore864/health-check-sample-service/blob/main/main.py) written in Python which listens on port 5000 serving a `/health` endpoint that:

- always returns success on the first access;
- 20% chance to fail;
- once fails, always fails after that with no possibility to recover.

```{note}
You will need a Ubuntu VM, Python 3.8+ and Flask to run this demo service:

```bash
git clone https://github.com/IronCore864/health-check-sample-service.git /path/to/your/working/directory
cd /path/to/your/working/directory
pip install -r requirements.txt
```

```{note}
Alternatively, you can install Flask with pip, then create a Python script with the content from the above repository, and put it at a location accessible by Pebble.
```

## Putting it all together

Suppose the sample service is located at `/home/ubuntu/work/health-check-sample-service/main.py`. Let's create a Pebble layer:

```yaml
summary: a simple layer
services:
    svc1:
        override: replace
        command: python3 /home/ubuntu/work/health-check-sample-service/main.py
        startup: enabled
        on-check-failure:
            svc1-up: restart
checks:
    svc1-up:
        override: replace
        period: 30s
        timeout: 1s
        http:
            url: http://127.0.0.1:5000/health
```

This is a simple layer that:

- starts the service `svc1` automatically when the Pebble daemon starts;
- configures a health check of `http` type with a 30-second interval and 1-second timeout;
- health check threshold defaults to 3;
- when the health check is considered done, restart service `svc1`.

First, let's start the Pebble daemon:

```{terminal}
:input: pebble run
2024-12-20T05:18:25.026Z [pebble] Started daemon.
2024-12-20T05:18:25.037Z [pebble] POST /v1/services 2.940959ms 202
2024-12-20T05:18:25.040Z [pebble] Service "svc1" starting: python3 /home/ubuntu/work/health-check-sample-service/main.py
2024-12-20T05:18:26.044Z [pebble] GET /v1/changes/2/wait 1.006686792s 200
2024-12-20T05:18:26.044Z [pebble] Started default services with change 2.
```

As we can see from the log, the service is started successfully, which can be verified by running `pebble services`:

```{terminal}
:input: pebble services
Service  Startup  Current  Since
svc1     enabled  active   today at 13:18 CST
```

If we wait for a while, the health check would fail:

```bash
2024-12-20T05:22:55.038Z [pebble] Check "svc1-up" failure 1/3: non-20x status code 500
2024-12-20T05:23:25.043Z [pebble] Check "svc1-up" failure 2/3: non-20x status code 500
2024-12-20T05:23:55.038Z [pebble] Check "svc1-up" failure 3/3: non-20x status code 500
```

And, since we configured the "restart on health check failure" feature, we can see from the logs that Pebble tries to restart it:

```bash
2024-12-20T05:23:55.038Z [pebble] Check "svc1-up" threshold 3 hit, triggering action and recovering
2024-12-20T05:23:55.038Z [pebble] Service "svc1" on-check-failure action is "restart", terminating process before restarting
2024-12-20T05:23:55.038Z [pebble] Change 1 task (Perform HTTP check "svc1-up") failed: non-20x status code 500
2024-12-20T05:23:55.065Z [pebble] Service "svc1" exited after check failure, restarting
2024-12-20T05:23:55.065Z [pebble] Service "svc1" on-check-failure action is "restart", waiting ~500ms before restart (backoff 1)
2024-12-20T05:23:55.595Z [pebble] Service "svc1" starting: python3 /home/ubuntu/work/health-check-sample-service/main.py
```

If we check the services again, we can see the service has been restarted, the "Since" time is updated to the new start time:

```{terminal}
:input: pebble services
Service  Startup  Current  Since
svc1     enabled  active   today at 13:23 CST
```

We can also confirm from [Changes and tasks](../reference/changes-and-tasks):

```{terminal}
:input: pebble changes
ID   Status  Spawn               Ready               Summary
1    Error   today at 13:18 CST  today at 13:23 CST  Perform HTTP check "svc1-up"
2    Done    today at 13:18 CST  today at 13:18 CST  Autostart service "svc1"
3    Done    today at 13:23 CST  today at 13:24 CST  Recover HTTP check "svc1-up"
```

## See more

- [Health checks](../reference/health-checks)
- [Layer specification](../reference/layer-specification)
- [Service lifecycle](../reference/service-lifecycle)
- [How to manage service dependencies](service-dependencies)
