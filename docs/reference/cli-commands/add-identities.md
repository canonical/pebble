(reference_pebble_add-identities_command)=
# add-identities command

The `add-identities` command is used to add new identities.

## Usage

<!-- START AUTOMATED OUTPUT -->
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

[add-identities command options]
      --from=   Path of YAML file to read identities from (required)
```
<!-- END AUTOMATED OUTPUT -->
