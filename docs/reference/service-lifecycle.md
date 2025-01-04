# Service lifecycle

Pebble manages the lifecycle of a service, including starting, stopping, and restarting it. Pebble also handles health checks, failures, and auto-restart with backoff. This is all achieved using a state machine with the following states:

- initial: The service's initial state.
- starting: The service is in the process of starting.
- running: The `okayDelay` (see below) period has passed, and the service runs normally.
- terminating: The service is being gracefully terminated.
- killing: The service is being forcibly killed.
- stopped: The service has stopped.
- backoff: The service will be put in the backoff state before the next start attempt if the service is configured to restart when it exits.
- exited: The service has exited (and won't be automatically restarted).

## Service start

A service begins in an "initial" state. Pebble tries to start the service's underlying process and transitions the service to the "starting" state.

## Start confirmation

Pebble waits for a short period (`okayDelay`, defaults to one second) after starting the service. If the service runs without exiting after the `okayDelay` period, it's considered successfully started, and the service's state is transitioned into "running".

No matter if the service is in the "starting" or "running" state, if you get the service, the status will be shown as "active". Read more in the [`pebble services`](#reference_pebble_services_command) command.

## Start failure

If the service exits quickly, an error along with the last logs are added to the task (see more in [Changes and tasks](/reference/changes-and-tasks.md)). This also ensures logs are accessible.

## Abort start

If the user interrupts the start process (e.g., with a SIGKILL), the service transitions to stopped, and a SIGKILL signal is sent to the underlying process.

## Auto-restart

By default, Pebble's service manager automatically restarts services that exit unexpectedly, regardless of whether the service is in the "starting" state (the `okayDelay` period has not passed) or in the "running" state (`okayDelay` has passed, and the service is considered to be "running").

Pebble considers a service to have exited unexpectedly if the exit code is non-zero.

You can fine-tune the auto-restart behaviour using the `on-success` and `on-failure` fields in a configuration layer. The possible values for these fields are:

* `restart`: restart the service and enter a restart-backoff loop (the default behaviour).
* `shutdown`: shut down and exit the Pebble daemon (with exit code 0 if the service exits successfully, exit code 10 otherwise)
  - `success-shutdown`: shut down with exit code 0 (valid only for `on-failure`)
  - `failure-shutdown`: shut down with exit code 10 (valid only for `on-success`)
* `ignore`: ignore the service exiting and do nothing further

## Backoff

Pebble implements a backoff mechanism that increases the delay before restarting the service after each failed attempt. This prevents a failing service from consuming excessive resources.

The `backoff-delay` defaults to half a second, the `backoff-factor` defaults to 2.0 (doubling), and the increasing delay is capped at `backoff-limit`, which defaults to 30 seconds. All of the three configurations can be customized, read more in [Layer specification](../reference/layer-specification).

With default settings for the above configuration, in `restart` mode, the first time a service exits, Pebble waits for half a second. If the service exits again, Pebble calculates the next backoff delay by multiplying the current delay by `backoff-factor`, which results in a 1-second delay. The next delay will be 2 seconds, then 4 seconds, and so on, capped at 30 seconds.

The `backoff-limit` value is also used as a "backoff reset" time. If the service stays running after a restart for `backoff-limit` seconds, the backoff process is reset and the delay reverts to `backoff-delay`.

## Auto-restart on health check failures

Pebble can be configured to automatically restart services based on health checks. To do so, use `on-check-failure` in the service configuration. Read more in [Health checks](health-checks).
