(reference_pebble_tasks_command)=
# tasks command

The `tasks` command is used to list a change's tasks.

## Usage

<!-- START AUTOMATED OUTPUT -->
```{terminal}
:input: pebble tasks --help
Usage:
  pebble tasks [tasks-OPTIONS] [<change-id>]

The tasks command displays a summary of tasks associated with an individual
change that happened recently.

[tasks command options]
      --abs-time       Display absolute times (in RFC 3339 format). Otherwise,
                       display relative times up to 60 days, then YYYY-MM-DD.
      --last=          Select last change of given type (install, refresh,
                       remove, try, auto-refresh, etc.). A question mark at the
                       end of the type means to do nothing (instead of
                       returning an error) if no change of the given type is
                       found. Note the question mark could need protecting from
                       the shell.

[tasks command arguments]
  <change-id>:         Change ID
```
<!-- END AUTOMATED OUTPUT -->

## Examples

To view tasks from the change with ID 3, run:

```{terminal}
   :input: pebble tasks 3
Status  Spawn                Ready                Summary
Done    today at 15:26 NZDT  today at 15:26 NZDT  Stop service "srv1"
Done    today at 15:26 NZDT  today at 15:26 NZDT  Stop service "srv2"
```

Read more: [Changes and tasks](../changes-and-tasks.md).
