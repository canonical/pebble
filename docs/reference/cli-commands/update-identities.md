(reference_pebble_update-identities_command)=
# update-identities command

The `update-identities` command is used to update or replace identities.

## Usage

<!-- START AUTOMATED OUTPUT -->
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
<!-- END AUTOMATED OUTPUT -->
