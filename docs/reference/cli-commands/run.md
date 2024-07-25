(reference_pebble_run_command)=
# run command

The run command starts Pebble and runs the configured environment.

## Usage

<!-- START AUTOMATED OUTPUT -->
```{terminal}
:input: pebble run --help
Usage:
  pebble run [run-OPTIONS]

The run command starts Pebble and runs the configured environment.

Additional arguments may be provided to the service command with the --args option, which
must be terminated with ";" unless there are no further program options.  These arguments
are appended to the end of the service command, and replace any default arguments defined
in the service plan. For example:

pebble run --args myservice --port 8080 \; --hold

[run command options]
          --create-dirs  Create Pebble directory on startup if it doesn't exist
          --hold         Do not start default services automatically
          --http=        Start HTTP API listening on this address (e.g., ":4000")
      -v, --verbose      Log all output from services to stdout
          --args=        Provide additional arguments to a service
```
<!-- END AUTOMATED OUTPUT -->
