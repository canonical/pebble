# How to use Pebble in containers

Pebble works well as a local service manager, but if running Pebble in a separate container, you can use the exec and file management APIs to coordinate with the remote system over the shared unix socket.

## Exec (one-shot commands)

Pebble's "exec" feature allows you to run arbitrary commands on the server. This is intended for short-running programs; the processes started with exec don't use the service manager.

For example, you could use `exec` to run pg_dump and create a PostgreSQL database backup:

```
$ pebble exec pg_dump mydb
--
-- PostgreSQL database dump
--
...
```

The exec feature uses WebSockets under the hood, and allows you to stream stdin to the process, as well as stream stdout and stderr back. When running `pebble exec`, you can specify the working directory to run in (`-w`), environment variables to set (`--env`), and the user and group to run as (`--uid`/`--user` and `--gid`/`--group`).

You can also apply a timeout with `--timeout`, for example:

```
$ pebble exec --timeout 1s -- sleep 3
error: cannot perform the following tasks:
- exec command "sleep" (timed out after 1s: context deadline exceeded)
```

## File management

Pebble provides various API calls and commands to manage files and directories on the server. The simplest way to use these is with the commands below, several of which should be familiar:

```
$ pebble ls <path>              # list file information (like "ls")
$ pebble mkdir <path>           # create a directory (like "mkdir")
$ pebble rm <path>              # remove a file or directory (like "rm")
$ pebble push <local> <remote>  # copy file to server (like "cp")
$ pebble pull <remote> <local>  # copy file from server (like "cp")
```
