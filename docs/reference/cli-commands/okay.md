(reference_pebble_okay_command)=
# okay command

The `okay` command is used to acknowledge notices and warnings.

## Usage

<!-- START AUTOMATED OUTPUT -->
```{terminal}
:input: pebble okay --help
Usage:
  pebble okay [okay-OPTIONS]

The okay command acknowledges warnings and notices that have been previously
listed using 'pebble warnings' or 'pebble notices', so that they are omitted
from future runs of either command. When a notice or warning is repeated, it
will again show up until the next 'pebble okay'.

[okay command options]
      --warnings    Only acknowledge warnings, not other notices
```
<!-- END AUTOMATED OUTPUT -->
