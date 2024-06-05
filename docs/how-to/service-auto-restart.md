# How to configure service auto-restart

Pebble's service manager automatically restarts services that exit unexpectedly. By default, this is done whether the exit code is zero or non-zero, but you can change this using the `on-success` and `on-failure` fields in a configuration layer. The possible values for these fields are:

* `restart`: restart the service and enter a restart-backoff loop (the default behaviour).
* `shutdown`: shut down and exit the Pebble daemon (with exit code 0 if the service exits successfully, exit code 10 otherwise)
  - `success-shutdown`: shut down with exit code 0 (valid only for `on-failure`)
  - `failure-shutdown`: shut down with exit code 10 (valid only for `on-success`)
* `ignore`: ignore the service exiting and do nothing further

In `restart` mode, the first time a service exits, Pebble waits the `backoff-delay`, which defaults to half a second. If the service exits again, Pebble calculates the next backoff delay by multiplying the current delay by `backoff-factor`, which defaults to 2.0 (doubling). The increasing delay is capped at `backoff-limit`, which defaults to 30 seconds.

The `backoff-limit` value is also used as a "backoff reset" time. If the service stays running after a restart for `backoff-limit` seconds, the backoff process is reset and the delay reverts to `backoff-delay`.
