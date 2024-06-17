(how_to_install_pebble)=
# How to install Pebble

To install the latest version of Pebble, you can choose any of the following methods:

- {ref}`install_pebble_binary`
- {ref}`install_pebble_from_source`

(install_pebble_binary)=
## Install the binary

To install the binary for the latest version of Pebble:

```{include} /reuse/common-blocks.md
   :start-after: Start: Install Pebble binary
   :end-before: End: Install Pebble binary
```

(install_pebble_from_source)=
## Install from source

To install the latest version of Pebble from source:

1. Follow the official Go documentation [here](https://go.dev/doc/install) to download and install Go.
2. After installing, you will want to add the `$GOBIN` directory to your `$PATH` so you can use the installed tools. For more information, refer to the [official documentation](https://go.dev/doc/install/source#environment).
3. Run `go install github.com/canonical/pebble/cmd/pebble@latest` to build and install Pebble.

Pebble is invoked using `pebble <command>`. To get more information:

* To see a help summary, type `pebble -h`.
* To see a short description of all commands, type `pebble help --all`.
* To see details for one command, type `pebble help <command>` or `pebble <command> -h`.

## Verify Pebble installation

```{include} /reuse/common-blocks.md
   :start-after: Start: Verify Pebble installation
   :end-before: End: Verify Pebble installation
```
