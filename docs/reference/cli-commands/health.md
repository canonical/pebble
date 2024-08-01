(reference_pebble_health_command)=
# health command

The `health` command is used to query health of checks.

## Usage

<!-- START AUTOMATED OUTPUT -->
```{terminal}
:input: pebble health --help
Usage:
  pebble health [health-OPTIONS] [<check>...]

The health command queries the health of configured checks.

It returns an exit code 0 if all the requested checks are healthy, or
an exit code 1 if at least one of the requested checks are unhealthy.

[health command options]
      --level=[alive|ready]   Check level to filter for
```
<!-- END AUTOMATED OUTPUT -->
