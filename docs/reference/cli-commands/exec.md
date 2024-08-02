(reference_pebble_exec_command)=
# exec command

The `exec` command is used to execute a remote command and wait for it to finish.

## Usage

<!-- START AUTOMATED OUTPUT -->
```{terminal}
:input: pebble exec --help
Usage:
  pebble exec [exec-OPTIONS] <command>

The exec command runs a remote command and waits for it to finish. The local
stdin is sent as the input to the remote process, while the remote stdout and
stderr are output locally.

To avoid confusion, exec options may be separated from the command and its
arguments using "--", for example:

pebble exec --timeout 10s -- echo -n foo bar

[exec command options]
      -w=              Working directory to run command in
          --env=       Environment variable to set (in 'FOO=bar' format)
          --uid=       User ID to run command as
          --user=      Username to run command as (user's UID must match uid if both present)
          --gid=       Group ID to run command as
          --group=     Group name to run command as (group's GID must match gid if both present)
          --timeout=   Timeout after which to terminate command
          --context=   Inherit the context of the named service (overridden by -w, --env, --uid/user, --gid/group)
      -t               Allocate remote pseudo-terminal and connect stdout to it (default if stdout is a TTY)
      -T               Disable remote pseudo-terminal allocation
      -i               Interactive mode: connect stdin to the pseudo-terminal (default if stdin and stdout are TTYs)
      -I               Disable interactive mode and use a pipe for stdin
```
<!-- END AUTOMATED OUTPUT -->

Read more: [Use Pebble in containers](../pebble-in-containers.md).
