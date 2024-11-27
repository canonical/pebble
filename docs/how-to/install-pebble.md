(how_to_install_pebble)=
# How to install Pebble

To install the latest version of Pebble, choose any of the following methods:

- {ref}`install_pebble_binary`
- {ref}`install_pebble_snap`
- {ref}`install_pebble_from_source`

(install_pebble_binary)=
## Install the binary

To install the binary for the latest version of Pebble:

```{include} /reuse/install.md
   :start-after: Start: Install Pebble binary
   :end-before: End: Install Pebble binary
```

(install_pebble_snap)=
## Install the snap

To install the latest version of Pebble from the Snap Store:

```
sudo snap install pebble --classic
```

For information about snaps, see the [snap documentation](https://snapcraft.io/docs).


(install_pebble_from_source)=
## Install from source

To install the latest version of Pebble from source:

1. Follow the official Go documentation [here](https://go.dev/doc/install) to download and install Go.
2. After installing, you will want to add the `$GOBIN` directory to your `$PATH` so you can use the installed tools. For more information, refer to the [official documentation](https://go.dev/doc/install/source#environment).
3. Run `go install github.com/canonical/pebble/cmd/pebble@latest` to build and install Pebble.

## Verify the Pebble installation

```{include} /reuse/verify.md
   :start-after: Start: Verify the Pebble installation
   :end-before: End: Verify the Pebble installation
```

Pebble is invoked using `pebble <command>`. For more information, see {ref}`reference_pebble_help_command`.
