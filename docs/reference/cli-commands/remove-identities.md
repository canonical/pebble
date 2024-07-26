(reference_pebble_remove-identities_command)=
# remove-identities command

The `remove-identities` command is used to remove identities.

## Usage

<!-- START AUTOMATED OUTPUT -->
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
<!-- END AUTOMATED OUTPUT -->
