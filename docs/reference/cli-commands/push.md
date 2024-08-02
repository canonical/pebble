(reference_pebble_push_command)=
# push command

The `push` command is used to transfer a file to the remote system.

## Usage

<!-- START AUTOMATED OUTPUT -->
```{terminal}
:input: pebble push --help
Usage:
  pebble push [push-OPTIONS] <local-path> <remote-path>

The push command transfers a file to the remote system.

[push command options]
      -p                   Create parent directories for the file
      -m=                  Override mode bits (3-digit octal)
          --uid=           Use specified user ID
          --user=          Use specified username
          --gid=           Use specified group ID
          --group=         Use specified group name
```
<!-- END AUTOMATED OUTPUT -->

Read more: [Use Pebble in containers](../pebble-in-containers.md).
