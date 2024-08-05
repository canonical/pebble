(reference_pebble_add_command)=
# add command

The `add` command is used to dynamically add a layer to the plan's layers.

## Usage

<!-- START AUTOMATED OUTPUT -->
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
```
<!-- END AUTOMATED OUTPUT -->
