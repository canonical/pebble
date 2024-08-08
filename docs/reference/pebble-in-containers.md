# Use Pebble in containers

Pebble works well as a local service manager, but if running Pebble in a separate container, you can use the exec and file management APIs to coordinate with the remote system over the shared unix socket.

## Run commands in a container

To run commands in a container, see {ref}`reference_pebble_exec_command`.

## File management

Pebble provides various API calls and commands to manage files and directories on the server.

The simplest way to use these is with the commands below, several of which should be familiar:

- {ref}`reference_pebble_ls_command`
- {ref}`reference_pebble_mkdir_command`
- {ref}`reference_pebble_rm_command`
- {ref}`reference_pebble_push_command`
- {ref}`reference_pebble_pull_command`
