(reference_pebble_run_command)=
# run command

The `run` command is used to run the service manager environment.

## Usage

<!-- START AUTOMATED OUTPUT -->
```{terminal}
:input: pebble run --help
Usage:
  pebble run [run-OPTIONS]

The run command starts Pebble and runs the configured environment.

Additional arguments may be provided to the service command with the --args
option, which must be terminated with ";" unless there are no further program
options. These arguments are appended to the end of the service command, and
replace any default arguments defined in the service plan. For example:

pebble run --args myservice --port 8080 \; --hold

[run command options]
          --create-dirs  Create Pebble directory on startup if it doesn't exist
          --hold         Do not start default services automatically
          --http=        Start HTTP API listening on this address (e.g.,
                         ":4000")
      -v, --verbose      Log all output from services to stdout
          --args=        Provide additional arguments to a service
          --identities=  Seed identities from file (like update-identities
                         --replace)
```
<!-- END AUTOMATED OUTPUT -->

## How it works

`pebble run` will start the Pebble daemon itself, as well as start all the services that are marked as `startup: enabled` in the layer configuration (if you don't want that, use `--hold`). For more detail on layer configuration, see [Layer specification](../layer-specification.md).

After the Pebble daemon starts, other Pebble commands may be used to interact with the running daemon, for example, in another terminal window.

## Arguments

To provide additional arguments to a service, use `--args <service> <args> ...`. If the `command` field in the service's plan has a `[ <default-arguments...> ]` list, the `--args` arguments will replace the defaults. If not, they will be appended to the command.

To indicate the end of an `--args` list, use a `;` (semicolon) terminator, which must be backslash-escaped if used in the shell. The terminator may be omitted if there are no other Pebble options that follow.

## Examples

If Pebble is installed and the `$PEBBLE` directory is set up, run the daemon by:

```{terminal}
 :input: pebble run
2022-10-26T01:18:26.904Z [pebble] Started daemon.
2022-10-26T01:18:26.921Z [pebble] POST /v1/services 15.53132ms 202
2022-10-26T01:18:26.921Z [pebble] Started default services with change 50.
2022-10-26T01:18:26.936Z [pebble] Service "srv1" starting: sleep 300
```

### Run the daemon with arguments

To start the daemon and pass additional arguments to "myservice", run:

```bash
pebble run --args myservice --verbose --foo "multi str arg"
```

To use args terminator to pass `--hold` to Pebble at the end of the line, run:

```bash
pebble run --args myservice --verbose \; --hold
```

To start the daemon and pass arguments to multiple services, run:

```bash
pebble run --args myservice1 --arg1 \; --args myservice2 --arg2
```

### Override the default configuration directory

To override the default configuration directory, set the `PEBBLE` environment variable first then run the daemon:

```bash
export PEBBLE=~/pebble
pebble run
```

### Initialise the `$PEBBLE` directory with a one-time copy

To initialise the `$PEBBLE` directory with the contents of another, in a one-time copy, set the `PEBBLE_COPY_ONCE` environment variable to the source directory.

This will only copy the contents if the target directory, `$PEBBLE`, is empty.