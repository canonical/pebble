(reference_pebble_changes_command)=
# changes command

The `changes` command is used to list system changes.

## Usage

<!-- START AUTOMATED OUTPUT -->
```{terminal}
:input: pebble changes --help
Usage:
  pebble changes [changes-OPTIONS] [<service>]

The changes command displays a summary of system changes performed recently.

[changes command options]
      --abs-time     Display absolute times (in RFC 3339 format). Otherwise,
                     display relative times up to 60 days, then YYYY-MM-DD.
```
<!-- END AUTOMATED OUTPUT -->

## Examples

Here is an example of `pebble changes`. You should get output similar to this:

```
$ pebble changes
ID  Status  Spawn                Ready                Summary
1   Done    today at 14:33 NZDT  today at 14:33 NZDT  Autostart service "srv1"
2   Done    today at 15:26 NZDT  today at 15:26 NZDT  Start service "srv2"
3   Done    today at 15:26 NZDT  today at 15:26 NZDT  Stop service "srv1" and 1 more
```

Read more: [Changes and tasks](../changes-and-tasks.md).
