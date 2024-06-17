Start: Install Pebble binary

1. Visit the [latest release page](https://github.com/canonical/pebble/releases/latest) to get the latest tag.
2. Run the following commands to download the file. Make sure to replace `v1.12.0` with the latest tag and `amd64` with your architecture.
    ```bash
    wget https://github.com/canonical/pebble/releases/download/v1.12.0/pebble_v1.12.0_linux_amd64.tar.gz
    tar zxvf pebble_v1.12.0_linux_amd64.tar.gz
    sudo mv pebble /usr/local/bin/
    ```
3. Extract the contents of the downloaded file by running:
    ```bash
    tar zxvf pebble_v1.12.0_linux_amd64.tar.gz
    ```
4. Install the Pebble binary. Make sure it's included in your system's `PATH` environment variable.
    ```bash
    sudo mv pebble /usr/local/bin/
    ```

End: Install Pebble binary

Start: Verify Pebble installation

Once the installation is complete, verify that `pebble` has been installed correctly by running:

```bash
pebble
```

This should produce output similar to the following:

```{terminal}
   :input: pebble
   :user: user
   :host: host
   :dir: ~
Pebble lets you control services and perform management actions on
the system that is running them.

Usage: pebble <command> [<options>...]

...
```

For more information, see {ref}`reference_pebble_help_command`.

End: Verify Pebble installation
