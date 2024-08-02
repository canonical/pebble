(reference_pebble_services_command)=
# services command

The services command lists status information about the services specified, or about all services if none are specified.

## Usage

<!-- START AUTOMATED OUTPUT -->
```{terminal}
:input: pebble services --help
Usage:
  pebble services [services-OPTIONS] [<service>...]

The services command lists status information about the services specified, or
about all services if none are specified.

[services command options]
      --abs-time     Display absolute times (in RFC 3339 format). Otherwise,
                     display relative times up to 60 days, then YYYY-MM-DD.
```
<!-- END AUTOMATED OUTPUT -->

## Examples

You can view the status of one or more services by using `pebble services`:

To show status of a single service:

```{terminal}
   :input: pebble services srv1       
Service  Startup  Current
srv1     enabled  active
```

To show status of all services:

```{terminal}
   :input: pebble services
Service  Startup   Current
srv1     enabled   active
srv2     disabled  inactive
```

The "Startup" column shows whether this service is automatically started when Pebble starts ("enabled" means auto-start, "disabled" means don't auto-start).

The "Current" column shows the current status of the service, and can be one of the following:

* `active`: starting or running
* `inactive`: not yet started, being stopped, or stopped
* `backoff`: in a [backoff-restart loop](../service-auto-restart.md)
* `error`: in an error state
