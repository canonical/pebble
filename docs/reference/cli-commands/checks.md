(reference_pebble_checks_command)=
# checks command

The `checks` command is used to query the status of configured health checks.

## Usage

<!-- START AUTOMATED OUTPUT -->
```{terminal}
:input: pebble checks --help
Usage:
  pebble checks [checks-OPTIONS] [<check>...]

The checks command lists status information about the configured health
checks, optionally filtered by level and check names provided as positional
arguments.

[checks command options]
      --level=[alive|ready]   Check level to filter for
```
<!-- END AUTOMATED OUTPUT -->
