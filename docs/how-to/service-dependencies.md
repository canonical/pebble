# How to manage service dependencies

Pebble takes service dependencies into account when starting and stopping services. When Pebble starts a service, it also starts the services which that service depends on (configured with `required`). Conversely, when stopping a service, Pebble also stops services which depend on that service.

For example, if service `nginx` requires `logger`, `pebble start nginx` will start both `nginx` and `logger` (in an undefined order). Running `pebble stop logger` will stop both `nginx` and `logger`; however, running `pebble stop nginx` will only stop `nginx` (`nginx` depends on `logger`, not the other way around).

When multiple services need to be started together, they're started in order according to the `before` and `after` configuration, waiting 1 second for each to ensure the command doesn't exit too quickly. The `before` option is a list of services that this service must start before (it may or may not `require` them). Or if it's easier to specify this ordering the other way around, `after` is a list of services that this service must start after.

Note that currently, `before` and `after` are of limited usefulness, because Pebble only waits 1 second before moving on to start the next service, with no additional checks that the previous service is operating correctly.

If the configuration of `requires`, `before`, and `after` for a service results in a cycle or "loop", an error will be returned when attempting to start or stop the service.
