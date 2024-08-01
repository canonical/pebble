(reference_pebble_help_command)=
# help command

Use the **help** command (`help` or `-h`) to get a summary or detailed
information about available `pebble` commands.

## Usage

To display a summary about Pebble and the available commands, run:

<!-- START AUTOMATED OUTPUT -->
```{terminal}
:input: pebble help
Pebble lets you control services and perform management actions on
the system that is running them.

Usage: pebble <command> [<options>...]

Commands can be classified as follows:

         Run: run
        Info: help, version
        Plan: add, plan
    Services: services, logs, start, restart, signal, stop, replan
      Checks: checks, health
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
<!-- END AUTOMATED OUTPUT -->

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
