(reference_pebble_notices_command)=
# notices command

The `notices` command is used to list notices.

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

By default, list notices with the current user ID or public notices. Admins
can use --users=all to view notice with any user ID, or --uid=UID to view
another user's notices.

[notices command options]
      --abs-time    Display absolute times (in RFC 3339 format). Otherwise,
                    display relative times up to 60 days, then YYYY-MM-DD.
      --users=      The only valid value is 'all', which lists notices with any
                    user ID (admin only; cannot be used with --uid)
      --uid=        Only list notices with this user ID (admin only; cannot be
                    used with --users)
      --type=       Only list notices of this type (multiple allowed)
      --key=        Only list notices with this key (multiple allowed)
      --timeout=    Wait up to this duration for matching notices to arrive
```
<!-- END AUTOMATED OUTPUT -->

## Examples

To fetch all notices:

```{terminal}
   :input: pebble notices
ID   User    Type    Key              First                Repeated             Occurrences
1    1000    custom  example.com/foo  today at 16:16 NZST  today at 16:16 NZST  3
2    public  custom  other.com/bar    today at 16:16 NZST  today at 16:16 NZST  1
```

Read more: [Notices](../notices.md).
