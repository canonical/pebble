---
tocdepth: 2
---

# CLI commands

The `pebble` command has the following subcommands, organised into logical groups:

* Run: [run](#reference_pebble_run_command)
* Info: [help](#reference_pebble_help_command), [version](#reference_pebble_version_command)
* Plan: [add](#reference_pebble_add_command), [plan](#reference_pebble_plan_command), [replan](#reference_pebble_replan_command)
* Services: [services](#reference_pebble_services_command), [logs](#reference_pebble_logs_command), [start](#reference_pebble_start_command), [restart](#reference_pebble_restart_command), [signal](#reference_pebble_signal_command), [stop](#reference_pebble_stop_command)
* Checks: [checks](#reference_pebble_checks_command), [start-checks](#reference_pebble_start_checks_command), [stop-checks](#reference_pebble_stop_checks_command), [health](#reference_pebble_health_command)
* Files: [push](#reference_pebble_push_command), [pull](#reference_pebble_pull_command), [ls](#reference_pebble_ls_command), [mkdir](#reference_pebble_mkdir_command), [rm](#reference_pebble_rm_command), [exec](#reference_pebble_exec_command)
* Changes: [changes](#reference_pebble_changes_command), [tasks](#reference_pebble_tasks_command)
* Notices: [warnings](#reference_pebble_warnings_command), [okay](#reference_pebble_okay_command), [notices](#reference_pebble_notices_command), [notice](#reference_pebble_notice_command), [notify](#reference_pebble_notify_command)
* Identities: [identities](#reference_pebble_identities_command), [identity](#reference_pebble_identity_command), [add-identities](#reference_pebble_add-identities_command), [update-identities](#reference_pebble_update-identities_command), [remove-identities](#reference_pebble_remove-identities_command)

The subcommands are listed alphabetically below.


(reference_pebble_add_command)=
## add

The `add` command is used to dynamically add a layer to the plan's layers.

<!-- START AUTOMATED OUTPUT FOR add -->
```{terminal}
:input: pebble add --help
Usage:
  pebble add [add-OPTIONS] <label> <layer-path>

The add command reads the plan's layer YAML from the path specified and
appends a layer with the given label to the plan's layers. If --combine
is specified, combine the layer with an existing layer that has the given
label (or append if the label is not found).

[add command options]
      --combine         Combine the new layer with an existing layer that has
                        the given label (default is to append)
      --inner           Allow appending a new layer inside an existing
                        subdirectory
```
<!-- END AUTOMATED OUTPUT FOR add -->


(reference_pebble_add-identities_command)=
## add-identities

The `add-identities` command is used to add new identities.

<!-- START AUTOMATED OUTPUT FOR add-identities -->
```{terminal}
:input: pebble add-identities --help
Usage:
  pebble add-identities [add-identities-OPTIONS]

The add-identities command adds one or more new identities.

The named identities must not yet exist.

For example, to add a local admin named "bob", use YAML like this:

> identities:
>     bob:
>         access: admin
>         local:
>             user-id: 42

To add an identity named "alice" with metrics access using HTTP basic
authentication:

> identities:
>     alice:
>         access: metrics
>         basic:
>             password: <password hash>

Use "openssl passwd -6" to generate a hashed password (sha512-crypt format).

[add-identities command options]
      --from=   Path of YAML file to read identities from (required)
```
<!-- END AUTOMATED OUTPUT FOR add-identities -->


(reference_pebble_changes_command)=
## changes

The `changes` command is used to list system changes.

<!-- START AUTOMATED OUTPUT FOR changes -->
```{terminal}
:input: pebble changes --help
Usage:
  pebble changes [changes-OPTIONS] [<service>]

The changes command displays a summary of system changes performed recently.

[changes command options]
      --abs-time     Display absolute times (in RFC 3339 format). Otherwise,
                     display relative times up to 60 days, then YYYY-MM-DD.
```
<!-- END AUTOMATED OUTPUT FOR changes -->

### Examples

Here is an example of `pebble changes`. You should get output similar to this:

```
$ pebble changes
ID  Status  Spawn                Ready                Summary
1   Done    today at 14:33 NZDT  today at 14:33 NZDT  Autostart service "srv1"
2   Done    today at 15:26 NZDT  today at 15:26 NZDT  Start service "srv2"
3   Done    today at 15:26 NZDT  today at 15:26 NZDT  Stop service "srv1" and 1 more
```

Read more: [Changes and tasks](changes-and-tasks.md).


(reference_pebble_checks_command)=
## checks

The `checks` command is used to query the status of configured health checks.

<!-- START AUTOMATED OUTPUT FOR checks -->
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
<!-- END AUTOMATED OUTPUT FOR checks -->


(reference_pebble_exec_command)=
## exec

The `exec` command is used to execute a remote command and wait for it to finish.

<!-- START AUTOMATED OUTPUT FOR exec -->
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
          --user=      Username to run command as (user's UID must match uid if
                       both present)
          --gid=       Group ID to run command as
          --group=     Group name to run command as (group's GID must match gid
                       if both present)
          --timeout=   Timeout after which to terminate command
          --context=   Inherit the context of the named service (overridden by
                       -w, --env, --uid/user, --gid/group)
      -t               Allocate remote pseudo-terminal and connect stdout to it
                       (default if stdout is a TTY)
      -T               Disable remote pseudo-terminal allocation
      -i               Interactive mode: connect stdin to the pseudo-terminal
                       (default if stdin and stdout are TTYs)
      -I               Disable interactive mode and use a pipe for stdin
```
<!-- END AUTOMATED OUTPUT FOR exec -->

### Examples

For example, you could use `exec` to run `pg_dump` and create a PostgreSQL database backup:

```{terminal}
   :input: pebble exec pg_dump mydb
--
-- PostgreSQL database dump
--
...
```

The exec feature uses WebSockets under the hood, and allows you to stream stdin to the process, as well as stream stdout and stderr back. When running `pebble exec`, you can specify the working directory to run in (`-w`), environment variables to set (`--env`), and the user and group to run as (`--uid`/`--user` and `--gid`/`--group`).

You can also apply a timeout with `--timeout`, for example:

```{terminal}
   :input: pebble exec --timeout 1s -- sleep 3
error: cannot perform the following tasks:
- exec command "sleep" (timed out after 1s: context deadline exceeded)
```

Read more: [How to use Pebble to manage remote systems](/how-to/manage-a-remote-system.md).


(reference_pebble_health_command)=
## health

The `health` command is used to query health of checks.

<!-- START AUTOMATED OUTPUT FOR health -->
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
<!-- END AUTOMATED OUTPUT FOR health -->


(reference_pebble_help_command)=
## help

Use the **help** command (`help` or `-h`) to get a summary or detailed
information about available `pebble` commands.

To display a summary about Pebble and the available commands, run:

<!-- START AUTOMATED OUTPUT FOR help -->
```{terminal}
:input: pebble help
Pebble lets you control services and perform management actions on
the system that is running them.

Usage: pebble <command> [<options>...]

Commands can be classified as follows:

         Run: run
        Info: help, version
        Plan: add, plan, replan
    Services: services, logs, start, restart, signal, stop
      Checks: checks, start-checks, stop-checks, health
       Files: push, pull, ls, mkdir, rm, exec
     Changes: changes, tasks
     Notices: warnings, okay, notices, notice, notify
  Identities: identities --help

Set the PEBBLE environment variable to override the configuration directory
(which defaults to /var/lib/pebble/default). Set PEBBLE_SOCKET to override
the unix socket used for the API (defaults to $PEBBLE/.pebble.socket).

For more information about a command, run 'pebble help <command>'.
For a short summary of all commands, run 'pebble help --all'.
```
<!-- END AUTOMATED OUTPUT FOR help -->

To display a short description of all available `pebble` commands, run:

```{terminal}
:input: pebble help --all
Pebble lets you control services and perform management actions on
the system that is running them.

Usage: pebble <command> [<options>...]

...
```

To get more details for a specific command, run:

```{terminal}
:input: pebble help <command>
```


(reference_pebble_identities_command)=
## identities

The `identities` command is used to list identities.

<!-- START AUTOMATED OUTPUT FOR identities -->
```{terminal}
:input: pebble identities --help
Usage:
  pebble identities [identities-OPTIONS]

The identities command lists all identities.

Other identity-related subcommands are as follows (use --help with any
subcommand for details):

pebble identity           Show a single identity
pebble add-identities     Add new identities
pebble update-identities  Update or replace identities
pebble remove-identities  Remove identities

[identities command options]
      --format=   Output format: "text" (default), "json", or "yaml".
```
<!-- END AUTOMATED OUTPUT FOR identities -->


(reference_pebble_identity_command)=
## identity

The `identity` command is used to show a single identity.

<!-- START AUTOMATED OUTPUT FOR identity -->
```{terminal}
:input: pebble identity --help
Usage:
  pebble identity <name>

The identity command shows details for a single identity in YAML format.
```
<!-- END AUTOMATED OUTPUT FOR identity -->


(reference_pebble_logs_command)=
## logs

The Pebble daemon's service manager stores the most recent stdout and stderr from each service, using a 100KB ring buffer per service. Each log line is prefixed with an RFC-3339 timestamp and the `[service-name]` in square brackets.

Logs are viewable via the logs API or using `pebble logs`:

<!-- START AUTOMATED OUTPUT FOR logs -->
```{terminal}
:input: pebble logs --help
Usage:
  pebble logs [logs-OPTIONS] [<service>...]

The logs command fetches buffered logs from the given services (or all services
if none are specified) and displays them in chronological order.

[logs command options]
      -f, --follow     Follow (tail) logs for given services until Ctrl-C is
                       pressed. If no services are specified, show logs from
                       all services running when the command starts.
          --format=    Output format: "text" (default) or "json" (JSON lines).
      -n=              Number of logs to show (before following); defaults to
                       30.
                       If 'all', show all buffered logs.
```
<!-- END AUTOMATED OUTPUT FOR logs -->

### Examples

To view logs, run:

```{terminal}
   :input: pebble logs
2022-11-14T01:35:06.979Z [srv1] Log 0 from srv1
2022-11-14T01:35:08.041Z [srv2] Log 0 from srv2
2022-11-14T01:35:09.982Z [srv1] Log 1 from srv1
```

To view existing logs and follow (tail) new output, use `-f` (press Ctrl-C to exit):

```{terminal}
   :input: pebble logs -f
2022-11-14T01:37:56.936Z [srv1] Log 0 from srv1
2022-11-14T01:37:57.978Z [srv2] Log 0 from srv2
2022-11-14T01:37:59.939Z [srv1] Log 1 from srv1
^C
```

You can output logs in JSON Lines format, using `--format=json`:

```{terminal}
   :input: pebble logs --format=json
{"time":"2022-11-14T01:39:10.886Z","service":"srv1","message":"Log 0 from srv1"}
{"time":"2022-11-14T01:39:11.943Z","service":"srv2","message":"Log 0 from srv2"}
{"time":"2022-11-14T01:39:13.889Z","service":"srv1","message":"Log 1 from srv1"}
```

If you want to also write service logs to Pebble's own stdout, run the daemon with `--verbose`:

```{terminal}
   :input: pebble run --verbose
2022-10-26T01:41:32.805Z [pebble] Started daemon.
2022-10-26T01:41:32.835Z [pebble] POST /v1/services 29.743632ms 202
2022-10-26T01:41:32.835Z [pebble] Started default services with change 7.
2022-10-26T01:41:32.849Z [pebble] Service "srv1" starting: python3 -u /path/to/srv1.py
2022-10-26T01:41:32.866Z [srv1] Log 0 from srv1
2022-10-26T01:41:35.870Z [srv1] Log 1 from srv1
2022-10-26T01:41:38.873Z [srv1] Log 2 from srv1
...
```


(reference_pebble_ls_command)=
## ls

The `ls` command is used to list path contents.

<!-- START AUTOMATED OUTPUT FOR ls -->
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
<!-- END AUTOMATED OUTPUT FOR ls -->

Read more: [How to use Pebble to manage remote systems](/how-to/manage-a-remote-system.md).


(reference_pebble_mkdir_command)=
## mkdir

The `mkdir` command is used to create a directory.

<!-- START AUTOMATED OUTPUT FOR mkdir -->
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
<!-- END AUTOMATED OUTPUT FOR mkdir -->

Read more: [How to use Pebble to manage remote systems](/how-to/manage-a-remote-system.md).


(reference_pebble_notice_command)=
## notice

The `notice` command is used to fetch a single notice.

<!-- START AUTOMATED OUTPUT FOR notice -->
```{terminal}
:input: pebble notice --help
Usage:
  pebble notice [notice-OPTIONS] <id-or-type> [<key>]

The notice command fetches a single notice, either by ID (1-arg variant), or
by unique type and key combination (2-arg variant).

[notice command options]
      --uid=            Look up notice from user with this UID (admin only;
                        2-arg variant only)
```
<!-- END AUTOMATED OUTPUT FOR notice -->

### Examples

You can fetch a notice either by ID or by type/key combination.

 To fetch the notice with ID "1":

```{terminal}
   :input: pebble notice 1
id: "1"
user-id: 1000
type: custom
key: example.com/foo
first-occurred: 2023-09-15T04:16:09.179395298Z
last-occurred: 2023-09-15T04:16:19.487035209Z
last-repeated: 2023-09-15T04:16:09.179395298Z
occurrences: 3
expire-after: 168h0m0s
```

To fetch the notice with type "custom" and key "example.com<span></span>/bar":

```{terminal}
   :input: pebble notice custom other.com/bar
id: "2"
user-id: public
type: custom
key: other.com/bar
first-occurred: 2023-09-15T04:16:17.180049768Z
last-occurred: 2023-09-15T04:16:17.180049768Z
last-repeated: 2023-09-15T04:16:17.180049768Z
occurrences: 1
last-data:
    name: value
    email: john@smith.com
expire-after: 168h0m0s
```

Read more: [Notices](notices.md).


(reference_pebble_notices_command)=
## notices

The `notices` command is used to list notices.

<!-- START AUTOMATED OUTPUT FOR notices -->
```{terminal}
:input: pebble notices --help
Usage:
  pebble notices [notices-OPTIONS]

The notices command lists notices not yet acknowledged, ordered by the
last-repeated time (oldest first). After it runs, the notices that were shown
may then be acknowledged by running 'pebble okay'. When a notice repeats, it
needs to be acknowledged again.

By default, list notices with the current user ID or public notices. Admins
can use --users=all to view notice with any user ID, or --uid=UID to view
another user's notices.

[notices command options]
      --abs-time    Display absolute times (in RFC 3339 format). Otherwise,
                    display relative times up to 60 days, then YYYY-MM-DD.
      --users=      The only valid value is 'all', which lists notices with any
                    user ID (admin only; cannot be used with --uid)
      --uid=        Only list notices with this user ID (admin only; cannot be
                    used with --users)
      --type=       Only list notices of this type (multiple allowed)
      --key=        Only list notices with this key (multiple allowed)
      --timeout=    Wait up to this duration for matching notices to arrive
```
<!-- END AUTOMATED OUTPUT FOR notices -->

### Examples

To fetch all notices:

```{terminal}
   :input: pebble notices
ID   User    Type    Key              First                Repeated             Occurrences
1    1000    custom  example.com/foo  today at 16:16 NZST  today at 16:16 NZST  3
2    public  custom  other.com/bar    today at 16:16 NZST  today at 16:16 NZST  1
```

Read more: [Notices](notices.md).


(reference_pebble_notify_command)=
## notify

The `notify` command is used to record a custom notice.

<!-- START AUTOMATED OUTPUT FOR notify -->
```{terminal}
:input: pebble notify --help
Usage:
  pebble notify [notify-OPTIONS] <key> [<name=value>...]

The notify command records a custom notice with the specified key and optional
data fields.

[notify command options]
      --repeat-after=   Prevent notice with same type and key from reoccurring
                        within this duration
```
<!-- END AUTOMATED OUTPUT FOR notify -->

### Examples

To record `custom` notices, use `pebble notify` -- the notice user ID will be set to the client's user ID:

```{terminal}
   :input: pebble notify example.com/foo
Recorded notice 1
```

Notify with two data fields:

```{terminal}
   :input: pebble notify other.com/bar name=value email=john@smith.com  
Recorded notice 2
```

Read more: [Notices](notices.md).


(reference_pebble_okay_command)=
## okay

The `okay` command is used to acknowledge notices and warnings.

<!-- START AUTOMATED OUTPUT FOR okay -->
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
<!-- END AUTOMATED OUTPUT FOR okay -->


(reference_pebble_plan_command)=
## plan

The `plan` command is used to show the plan with layers combined.

<!-- START AUTOMATED OUTPUT FOR plan -->
```{terminal}
:input: pebble plan --help
Usage:
  pebble plan

The plan command prints out the effective configuration of Pebble in YAML
format. Layers are combined according to the override rules defined in them.
```
<!-- END AUTOMATED OUTPUT FOR plan -->


(reference_pebble_pull_command)=
## pull

The `pull` command is used to retrieve a file from the remote system.

<!-- START AUTOMATED OUTPUT FOR pull -->
```{terminal}
:input: pebble pull --help
Usage:
  pebble pull <remote-path> <local-path>

The pull command retrieves a file from the remote system.
```
<!-- END AUTOMATED OUTPUT FOR pull -->

Read more: [How to use Pebble to manage remote systems](/how-to/manage-a-remote-system.md).


(reference_pebble_push_command)=
## push

The `push` command is used to transfer a file to the remote system.

<!-- START AUTOMATED OUTPUT FOR push -->
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
<!-- END AUTOMATED OUTPUT FOR push -->

Read more: [How to use Pebble to manage remote systems](/how-to/manage-a-remote-system.md).


(reference_pebble_remove-identities_command)=
## remove-identities

The `remove-identities` command is used to remove identities.

<!-- START AUTOMATED OUTPUT FOR remove-identities -->
```{terminal}
:input: pebble remove-identities --help
Usage:
  pebble remove-identities [remove-identities-OPTIONS]

The remove-identities command removes one or more identities.

The named identities must exist. The named identity entries must be null in
the YAML input. For example, to remove "alice" and "bob", use this YAML:

> identities:
>     alice: null
>     bob: null

[remove-identities command options]
      --from=   Path of YAML file to read identities from (required)
```
<!-- END AUTOMATED OUTPUT FOR remove-identities -->


(reference_pebble_replan_command)=
## replan

The `replan` command starts, stops, or restarts services that have changed, so that running services exactly match the desired configuration in the current plan.

<!-- START AUTOMATED OUTPUT FOR replan -->
```{terminal}
:input: pebble replan --help
Usage:
  pebble replan [replan-OPTIONS]

The replan command starts, stops, or restarts services and checks that have
changed, so that running services and checks exactly match the desired
configuration in the current plan.

[replan command options]
      --no-wait    Do not wait for the operation to finish but just print the
                   change id.
```
<!-- END AUTOMATED OUTPUT FOR replan -->

### How it works

When you update service configuration (by adding a layer), the services changed won't be automatically restarted. `pebble replan ` restarts them and brings the service state in sync with the new configuration.

For `startup: enabled` services that are running:

- If the service hasn't changed configuration since it started, replan does nothing to the service.
- If the service has changed configuration since it started, replan restarts the service.

Replan also starts any `startup: enabled` services that have not yet been started, or that have been manually stopped.

### Examples

Here is an example, where `srv1` is a service that has `startup: enabled`, and `srv2` does not:

```{terminal}
   :input: pebble replan
2023-04-25T15:06:50+02:00 INFO Service "srv1" already started.
```

Update "srv1" config:

```{terminal}
   :input: pebble add lay1 layer.yaml
Layer "lay1" added successfully from "layer.yaml"
```

Replan:

```{terminal}
   :input: pebble replan
Stop service "srv1"
Start service "srv1"
```

Change "srv2" to "startup: enabled"

```{terminal}
   :input: pebble add lay2 layer.yaml
Layer "lay2" added successfully from "layer.yaml"
```

Replan again:

```{terminal}
   :input: pebble replan
2023-04-25T15:11:22+02:00 INFO Service "srv1" already started.
Start service "srv2"
```

```{note}
If you want to force a service to restart even if its service configuration hasn't changed, use `pebble restart <service>`.
```


(reference_pebble_restart_command)=
## restart

The `restart` command is used to restart a service.

<!-- START AUTOMATED OUTPUT FOR restart -->
```{terminal}
:input: pebble restart --help
Usage:
  pebble restart [restart-OPTIONS] <service>...

The restart command restarts the named service(s) in the correct order.

[restart command options]
      --no-wait      Do not wait for the operation to finish but just print the
                     change id.
```
<!-- END AUTOMATED OUTPUT FOR restart -->


(reference_pebble_rm_command)=
## rm

The `rm` command is used to remove a file or directory.

<!-- START AUTOMATED OUTPUT FOR rm -->
```{terminal}
:input: pebble rm --help
Usage:
  pebble rm [rm-OPTIONS] <path>

The rm command removes a file or directory.

[rm command options]
      -r            Remove all files and directories recursively in the
                    specified path
```
<!-- END AUTOMATED OUTPUT FOR rm -->

Read more: [How to use Pebble to manage remote systems](/how-to/manage-a-remote-system.md).


(reference_pebble_run_command)=
## run

The `run` command is used to run the service manager environment.

<!-- START AUTOMATED OUTPUT FOR run -->
```{terminal}
:input: pebble run --help
Usage:
  pebble run [run-OPTIONS]

The run command starts Pebble and runs the configured environment.

Additional arguments may be provided to the service command with the --args
option, which must be terminated with ";" unless there are no further program
options. These arguments are appended to the end of the service command, and
replace any default arguments defined in the service plan. For example:

pebble run --args myservice --port 8080 \; --hold

[run command options]
          --create-dirs  Create Pebble directory on startup if it doesn't exist
          --hold         Do not start default services automatically
          --http=        Start HTTP API listening on this address (e.g.,
                         ":4000") and expose open-access endpoints
      -v, --verbose      Log all output from services to stdout
          --args=        Provide additional arguments to a service
          --identities=  Seed identities from file (like update-identities
                         --replace)
```
<!-- END AUTOMATED OUTPUT FOR run -->

### How it works

`pebble run` will start the Pebble daemon itself, as well as start all the services that are marked as `startup: enabled` in the layer configuration (if you don't want that, use `--hold`). For more detail on layer configuration, see [Layer specification](layer-specification.md).

After the Pebble daemon starts, other Pebble commands may be used to interact with the running daemon, for example, in another terminal window.

### Environment variables

* `PEBBLE` - To override the default configuration directory, set the `PEBBLE` environment variable first then run the daemon. For example:

    ```bash
    export PEBBLE=~/pebble
    pebble run
    ```

* `PEBBLE_COPY_ONCE` - To initialise the `$PEBBLE` directory with the contents of another, in a one-time copy, set the `PEBBLE_COPY_ONCE` environment variable to the source directory.

    This will only copy the contents if the target directory, `$PEBBLE`, is empty.

### Arguments

To provide additional arguments to a service, use `--args <service> <args> ...`. If the `command` field in the service's plan has a `[ <default-arguments...> ]` list, the `--args` arguments will replace the defaults. If not, they will be appended to the command.

To indicate the end of an `--args` list, use a `;` (semicolon) terminator, which must be backslash-escaped if used in the shell. The terminator may be omitted if there are no other Pebble options that follow.

### Examples

If Pebble is installed and the `$PEBBLE` directory is set up, run the daemon by:

```{terminal}
 :input: pebble run
2022-10-26T01:18:26.904Z [pebble] Started daemon.
2022-10-26T01:18:26.921Z [pebble] POST /v1/services 15.53132ms 202
2022-10-26T01:18:26.921Z [pebble] Started default services with change 50.
2022-10-26T01:18:26.936Z [pebble] Service "srv1" starting: sleep 300
```

More ways to run the daemon:

* Pass additional arguments to a service called `myservice`:

    ```bash
    pebble run --args myservice --verbose --foo "multi str arg"
    ```

* Use args terminator to pass `--hold` to Pebble at the end of the line:

    ```bash
    pebble run --args myservice --verbose \; --hold
    ```

* Pass arguments to multiple services:

    ```bash
    pebble run --args myservice1 --arg1 \; --args myservice2 --arg2
    ```


(reference_pebble_services_command)=
## services

The `services` command lists status information about the services specified, or about all services if none are specified.

<!-- START AUTOMATED OUTPUT FOR services -->
```{terminal}
:input: pebble services --help
Usage:
  pebble services [services-OPTIONS] [<service>...]

The services command lists status information about the services specified, or
about all services if none are specified.

[services command options]
      --abs-time     Display absolute times (in RFC 3339 format). Otherwise,
                     display relative times up to 60 days, then YYYY-MM-DD.
```
<!-- END AUTOMATED OUTPUT FOR services -->

### Examples

You can view the status of one or more services by using `pebble services`:

To show status of a single service:

```{terminal}
   :input: pebble services srv1       
Service  Startup  Current
srv1     enabled  active
```

To show status of all services:

```{terminal}
   :input: pebble services
Service  Startup   Current
srv1     enabled   active
srv2     disabled  inactive
```

The "Startup" column shows whether this service is automatically started when Pebble starts ("enabled" means auto-start, "disabled" means don't auto-start).

The "Current" column shows the current status of the service, and can be one of the following:

* `active`: starting or running
* `inactive`: not yet started, being stopped, or stopped
* `backoff`: in a [backoff-restart loop](service-auto-restart.md)
* `error`: in an error state


(reference_pebble_signal_command)=
## signal

The `signal` command is used to send a signal to one or more running services.

<!-- START AUTOMATED OUTPUT FOR signal -->
```{terminal}
:input: pebble signal --help
Usage:
  pebble signal <SIGNAL> <service>...

The signal command sends a signal to one or more running services. The signal
name must be uppercase, for example:

pebble signal HUP mysql nginx
```
<!-- END AUTOMATED OUTPUT FOR signal -->


(reference_pebble_start_command)=
## start

The `start` command starts the service with the provided name and any other services it depends on, in the correct order.

<!-- START AUTOMATED OUTPUT FOR start -->
```{terminal}
:input: pebble start --help
Usage:
  pebble start [start-OPTIONS] <service>...

The start command starts the service with the provided name and
any other services it depends on, in the correct order.

[start command options]
      --no-wait      Do not wait for the operation to finish but just print the
                     change id.
```
<!-- END AUTOMATED OUTPUT FOR start -->

### How it works

- If the command is still running at the end of the 1 second window, the start is considered successful.
- If the command exits within the 1 second window, Pebble retries the command after a configurable backoff, using the restart logic described in [](service-auto-restart.md). If one of the started services exits within the 1 second window, `pebble start` prints an appropriate error message and exits with an error.

### Examples

To start specific services, run `pebble start` followed by one or more service names. For example, to start two services named "srv1" and "srv2" (and any dependencies), run:

```bash
pebble start srv1 srv2
```


(reference_pebble_start_checks_command)=
## start-checks

The `start-checks` command starts the checks with the provided names.

<!-- START AUTOMATED OUTPUT FOR start-checks -->
```{terminal}
:input: pebble start-checks --help
Usage:
  pebble start-checks <check>...

The start-checks command starts the configured health checks provided as
positional arguments. For any checks that are already active, the command
has no effect.
```
<!-- END AUTOMATED OUTPUT FOR start-checks -->

### Examples

To start specific checks, run `pebble start-checks` followed by one or more check names. For example, to start two checks named "chk1" and "chk2", run:

```bash
pebble start-checks chk1 chk2
```

(reference_pebble_stop_command)=
## stop

The `stop` command stops the service with the provided name and any other service that depends on it, in the correct order.

<!-- START AUTOMATED OUTPUT FOR stop -->
```{terminal}
:input: pebble stop --help
Usage:
  pebble stop [stop-OPTIONS] <service>...

The stop command stops the service with the provided name and
any other service that depends on it, in the correct order.

[stop command options]
      --no-wait      Do not wait for the operation to finish but just print the
                     change id.
```
<!-- END AUTOMATED OUTPUT FOR stop -->

### How it works

When stopping a service, Pebble sends SIGTERM to the service's process group, and waits up to 5 seconds. If the command hasn't exited within that time window, Pebble sends SIGKILL to the service's process group and waits up to 5 more seconds. If the command exits within that 10-second time window, the stop is considered successful, otherwise `pebble stop` will exit with an error, regardless of the `on-failure` value.

### Examples

To stop specific services, use `pebble stop` followed by one or more service names. The following example stops one service named "srv1":

```bash
pebble stop srv1
```


(reference_pebble_stop_checks_command)=
## stop-checks

The `stop-checks` command stops the checks with the provided names.

<!-- START AUTOMATED OUTPUT FOR stop-checks -->
```{terminal}
:input: pebble stop-checks --help
Usage:
  pebble stop-checks <check>...

The stop-checks command stops the configured health checks provided as
positional arguments. For any checks that are inactive, the command has
no effect.
```
<!-- END AUTOMATED OUTPUT FOR stop-checks -->

### Examples

To stop specific checks, use `pebble stop-checks` followed by one or more check names. The following example stops one check named "chk1":

```bash
pebble stop-checks chk1
```


(reference_pebble_tasks_command)=
## tasks

The `tasks` command is used to list a change's tasks.

<!-- START AUTOMATED OUTPUT FOR tasks -->
```{terminal}
:input: pebble tasks --help
Usage:
  pebble tasks [tasks-OPTIONS] [<change-id>]

The tasks command displays a summary of tasks associated with an individual
change that happened recently.

[tasks command options]
      --abs-time       Display absolute times (in RFC 3339 format). Otherwise,
                       display relative times up to 60 days, then YYYY-MM-DD.
      --last=          Select last change of given type (install, refresh,
                       remove, try, auto-refresh, etc.). A question mark at the
                       end of the type means to do nothing (instead of
                       returning an error) if no change of the given type is
                       found. Note the question mark could need protecting from
                       the shell.

[tasks command arguments]
  <change-id>:         Change ID
```
<!-- END AUTOMATED OUTPUT FOR tasks -->

### Examples

To view tasks from the change with ID 3, run:

```{terminal}
   :input: pebble tasks 3
Status  Spawn                Ready                Summary
Done    today at 15:26 NZDT  today at 15:26 NZDT  Stop service "srv1"
Done    today at 15:26 NZDT  today at 15:26 NZDT  Stop service "srv2"
```

Read more: [Changes and tasks](changes-and-tasks.md).


(reference_pebble_update-identities_command)=
## update-identities

The `update-identities` command is used to update or replace identities.

<!-- START AUTOMATED OUTPUT FOR update-identities -->
```{terminal}
:input: pebble update-identities --help
Usage:
  pebble update-identities [update-identities-OPTIONS]

The update-identities command updates or replaces one or more identities.

By default, the named identities must already exist and are updated.

If --replace is specified, update-identities operates differently: if a named
identity exists, it will be updated. If it does not exist, it will be added.
If a named identity is null in the YAML input, that identity will be removed.
For example, to add or update "alice" and ensure "bob" is removed, use
--replace with YAML like this:

> identities:
>     alice:
>         access: admin
>         local:
>             user-id: 1000
>     bob: null

[update-identities command options]
      --from=      Path of YAML file to read identities from (required)
      --replace    Replace (add or update) identities; remove null identities
```
<!-- END AUTOMATED OUTPUT FOR update-identities -->


(reference_pebble_version_command)=
## version

The `version` command is used to show version details.

<!-- START AUTOMATED OUTPUT FOR version -->
```{terminal}
:input: pebble version --help
Usage:
  pebble version [version-OPTIONS]

The version command displays the versions of the running client and server.

[version command options]
      --client    Only display the client version
```
<!-- END AUTOMATED OUTPUT FOR version -->


(reference_pebble_warnings_command)=
## warnings

The `warnings` command is used to list warnings.

<!-- START AUTOMATED OUTPUT FOR warnings -->
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
<!-- END AUTOMATED OUTPUT FOR warnings -->
