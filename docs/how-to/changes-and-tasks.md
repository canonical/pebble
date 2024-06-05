# How to use changes and tasks

When Pebble performs a (potentially invasive or long-running) operation such as starting or stopping a service, it records a "change" object with one or more "tasks" in it. The daemon records this state in a JSON file on disk at `$PEBBLE/.pebble.state`.

To see recent changes, for this or previous server runs, use `pebble changes`. You might see something like this:

```
$ pebble changes
ID  Status  Spawn                Ready                Summary
1   Done    today at 14:33 NZDT  today at 14:33 NZDT  Autostart service "srv1"
2   Done    today at 15:26 NZDT  today at 15:26 NZDT  Start service "srv2"
3   Done    today at 15:26 NZDT  today at 15:26 NZDT  Stop service "srv1" and 1 more
```

To drill down and see the tasks that make up a change, use `pebble tasks <change-id>`:

```
$ pebble tasks 3
Status  Spawn                Ready                Summary
Done    today at 15:26 NZDT  today at 15:26 NZDT  Stop service "srv1"
Done    today at 15:26 NZDT  today at 15:26 NZDT  Stop service "srv2"
```
