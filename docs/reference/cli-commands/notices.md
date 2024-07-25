# Notices command

The notices command lists notices not yet acknowledged, ordered by the

## Usage

<!-- START AUTOMATED OUTPUT -->
```{terminal}
:input: pebble notices --help
Usage:
  pebble notices [notices-OPTIONS]

The notices command lists notices not yet acknowledged, ordered by the
last-repeated time (oldest first). After it runs, the notices that were shown
may then be acknowledged by running 'pebble okay'. When a notice repeats, it
needs to be acknowledged again.

[notices command options]
      --abs-time    Display absolute times (in RFC 3339 format). Otherwise, display relative times up to 60 days, then YYYY-MM-DD.
      --users=      Show all notices with any user ID (admin only; cannot be used with --uid)
      --uid=        Only list notices with this user ID (admin only; cannot be used with --users)
      --type=       Only list notices of this type (multiple allowed)
      --key=        Only list notices with this key (multiple allowed)
      --timeout=    Wait up to this duration for matching notices to arrive
```
<!-- END AUTOMATED OUTPUT -->
