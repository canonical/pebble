(reference_pebble_warnings_command)=
# warnings command

The `warnings` command is used to list warnings.

## Usage

<!-- START AUTOMATED OUTPUT -->
```{terminal}
:input: pebble warnings --help
Usage:
  pebble warnings [warnings-OPTIONS]

The warnings command lists the warnings that have been reported to the system.

Once warnings have been listed with 'pebble warnings', 'pebble okay' may be
used to silence them. A warning that's been silenced in this way will not be
listed again unless it happens again, _and_ a cooldown time has passed.

Warnings expire automatically, and once expired they are forgotten.

[warnings command options]
      --abs-time                      Display absolute times (in RFC 3339
                                      format). Otherwise, display relative
                                      times up to 60 days, then YYYY-MM-DD.
      --unicode=[auto|never|always]   Use a little bit of Unicode to improve
                                      legibility. (default: auto)
      --all                           Show all warnings
      --verbose                       Show more information
```
<!-- END AUTOMATED OUTPUT -->
