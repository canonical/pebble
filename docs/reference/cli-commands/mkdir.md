(reference_pebble_mkdir_command)=
# mkdir command

The `mkdir` command is used to create a directory.

## Usage

<!-- START AUTOMATED OUTPUT -->
```{terminal}
:input: pebble mkdir --help
Usage:
  pebble mkdir [mkdir-OPTIONS] <path>

The mkdir command creates the specified directory.

[mkdir command options]
      -p            Create parent directories as needed, and don't fail if path
                    already exists
      -m=           Override mode bits (3-digit octal)
          --uid=    Use specified user ID
          --user=   Use specified username
          --gid=    Use specified group ID
          --group=  Use specified group name
```
<!-- END AUTOMATED OUTPUT -->

Read more: [Use Pebble in containers](../pebble-in-containers.md).
