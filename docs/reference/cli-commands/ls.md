(reference_pebble_ls_command)=
# ls command

The `ls` command is used to list path contents.

## Usage

<!-- START AUTOMATED OUTPUT -->
```{terminal}
:input: pebble ls --help
Usage:
  pebble ls [ls-OPTIONS] <path>

The ls command lists entries in the filesystem at the specified path. A glob
pattern
may be specified for the last path element.

[ls command options]
          --abs-time  Display absolute times (in RFC 3339 format). Otherwise,
                      display relative times up to 60 days, then YYYY-MM-DD.
      -d              List matching entries themselves, not directory contents
      -l              Use a long listing format
```
<!-- END AUTOMATED OUTPUT -->

Read more: [Use Pebble in containers](../pebble-in-containers.md).
