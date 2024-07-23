# Pebble start command

The start command starts the service with the provided name and any other services it depends on, in the correct order.

## Usage

```{terminal}
   :input: pebble start --help
Usage:
  pebble services [services-OPTIONS] [<service>...]

The services command lists status information about the services specified, or
about all services if none are specified.

[services command options]
      --abs-time     Display absolute times (in RFC 3339 format). Otherwise, display relative times up to 60 days, then YYYY-MM-DD.
```

## How it works

When starting a service, Pebble executes the service's `command`, and waits 1 second to ensure the command doesn't exit too quickly. Assuming the command doesn't exit within that time window, the start is considered successful, otherwise `pebble start` will exit with an error, regardless of the `on-failure` value.

## Examples

To start specific services, run `pebble start` followed by one or more service names. For example, to start two services named "srv1" and "srv2" (and any dependencies), run:

```bash
pebble start srv1 srv2
```
