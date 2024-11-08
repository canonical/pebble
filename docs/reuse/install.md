---
nosearch: true
---

Start: Install Pebble binary

1. Visit the [latest release page](https://github.com/canonical/pebble/releases/latest) to determine the latest tag, for example, `v1.12.0`.
2. Run the following command to download the file. Make sure to replace `v1.12.0` with the latest tag and `amd64` with your architecture.
    ```bash
    wget https://github.com/canonical/pebble/releases/download/v1.12.0/pebble_v1.12.0_linux_amd64.tar.gz
    ```
3. Extract the contents of the downloaded file by running:
    ```bash
    tar zxvf pebble_v1.12.0_linux_amd64.tar.gz
    ```
4. Install the Pebble binary. Make sure the installation directory is included in your system's `PATH` environment variable.
    ```bash
    sudo mv pebble /usr/local/bin/
    ```

End: Install Pebble binary
