# Pebble Environment Variables

## PEBBLE

Pebble's configuration directory. Defaults to `/var/lib/pebble/default` if not specified.

The `$PEBBLE` directory must contain a `layers/` subdirectory that holds a stack of configuration files. See [general model](../explanation/general-model) for more information.

## PEBBLE_COPY_ONCE

To initialise the `$PEBBLE` directory with the contents of another, in a one-time copy, set the `PEBBLE_COPY_ONCE` environment variable to the source directory.

This will only copy the contents if the target directory, `$PEBBLE`, is empty.

## PEBBLE_DEBUG

If set to "1", debug logs will be printed to stderr.

## PEBBLE_SOCKET

Pebble socket path. Defaults to `$PEBBLE/.pebble.socket` if not specified, or `/var/lib/pebble/default/.pebble.socket` if `PEBBLE` is not set.

## PEBBLE_VERBOSE

If set to "1", the Pebble daemon writes service logs to stdout.

For `pebble run`, either `PEBBLE_VERBOSE=1` or the `--verbose` flag turns on verbose logging, with the command line flag overriding the environment variable.

For `pebble enter exec`, the `--verbose` flag is currently disallowed. However, `pebble enter` (including `pebble enter exec`) still respects the `PEBBLE_VERBOSE=1` environment variable: the user should know how their applications behave, and that they're okay to use with verbose logging turned on.

## XDG_CONFIG_HOME

The [XDG configuration directory](https://specifications.freedesktop.org/basedir-spec/latest/#basics). Certain Pebble CLI commands create or use data files in `$XDG_CONFIG_HOME/pebble`. Defaults to `$HOME/.config` if not specified.
