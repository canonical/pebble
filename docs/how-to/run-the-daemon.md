# How to run the daemon (server)

If Pebble is installed and the `$PEBBLE` directory is set up, running the daemon is easy:

```
$ pebble run
2022-10-26T01:18:26.904Z [pebble] Started daemon.
2022-10-26T01:18:26.921Z [pebble] POST /v1/services 15.53132ms 202
2022-10-26T01:18:26.921Z [pebble] Started default services with change 50.
2022-10-26T01:18:26.936Z [pebble] Service "srv1" starting: sleep 300
```

This will start the Pebble daemon itself, as well as starting all the services that are marked as `startup: enabled` (if you don't want that, use `--hold`). Then other Pebble commands may be used to interact with the running daemon, for example, in another terminal window.

To provide additional arguments to a service, use `--args <service> <args> ...`. If the `command` field in the service's plan has a `[ <default-arguments...> ]` list, the `--args` arguments will replace the defaults. If not, they will be appended to the command.

To indicate the end of an `--args` list, use a `;` (semicolon) terminator, which must be backslash-escaped if used in the shell. The terminator may be omitted if there are no other Pebble options that follow.

For example:

```
# Start the daemon and pass additional arguments to "myservice".
$ pebble run --args myservice --verbose --foo "multi str arg"

# Use args terminator to pass --hold to Pebble at the end of the line.
$ pebble run --args myservice --verbose \; --hold

# Start the daemon and pass arguments to multiple services.
$ pebble run --args myservice1 --arg1 \; --args myservice2 --arg2
```

To override the default configuration directory, set the `PEBBLE` environment variable when running:

```
$ export PEBBLE=~/pebble
pebble run
2022-10-26T01:18:26.904Z [pebble] Started daemon.
...
```

To initialise the `$PEBBLE` directory with the contents of another, in a one time copy, set the `PEBBLE_COPY_ONCE` environment variable to the source directory. This will only copy the contents if the target directory, `$PEBBLE`, is empty.
