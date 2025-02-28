# Pebble Environment Variables

## NOTIFY_SOCKET

If the `NOTIFY_SOCKET` environment variable is set, Pebble will send state string notifications to systemd. Specifically:

- When Pebble daemon starts, it sends "READY=1", which tells systemd that Pebble startup is finished.
- When Pebble daemon stops, it sends "STOPPING=1", which tells systemd that Pebble is beginning its shutdown. 

## PEBBLE

Pebble's configuration directory. Defaults to "/var/lib/pebble/default" if not specified.

The `$PEBBLE` directory must contain a `layers/` subdirectory that holds a stack of configuration files. See [general model](../explanation/general-model) for more information.

## PEBBLE_COPY_ONCE

If specified, when Pebble daemon starts, Pebble copies the directory specified by `PEBBLE_COPY_ONCE` to the Pebble directory once.

## PEBBLE_DEBUG

If set to "1", debug logs will be printed.

## PEBBLE_REBOOT_DELAY

A duration string for the delay of reboot. Used for tests.

Reboot delay defaults to 1 minute if `PEBBLE_REBOOT_DELAY` is not set.

## PEBBLE_SOCKET

Pebble socket path. Defaults to `$PEBBLE/.pebble.socket` if not specified.

## PEBBLE_VERBOSE

If set to "1", write service logs to Pebble's stdout.

For `pebble run`, allow verbose logging mode (log all output from services to stdout) to be enabled when starting the daemon by setting the environment variable `PEBBLE_VERBOSE=1`.  This is in addition to the existing way of enabling verbose mode with a command line argument: `pebble run --verbose`.

For pebble enter exec, the `--verbose flag` is currently disabled. However, `pebble enter` (including `pebble enter exec`) would still respect `PEBBLE_VERBOSE=1`: the user should know how their applications behave and it’s okay to use with verbose logging turned on.

## SPREAD_SYSTEM

If set, Pebble will think we are running tests, and the progress bar for commands `autorestart`, `replan`, `restart`, `start` and `stop` will be disabled.

## UNSAFE_IO

If set to "1," sync for testing is disabled. This brings massive improvements on certain filesystems (like btrfs) and noticeable improvements in all unit tests in general.

## WATCHDOG_USEC

If the `WATCHDOG_USEC` environment variable is set, systemd expects notifications from Pebble. Systemd will usually terminate a service when it does not get a notification message within the specified time after startup and after each previous message. It is recommended to send a keep-alive notification message every half of the time (in microseconds) specified by `WATCHDOG_USEC`. Notification messages are sent with `sd_notify` with a message string of "WATCHDOG=1".

## XDG_CONFIG_HOME

Pebble CLI state path. Defaults to "$HOME/.config" if not specified.
