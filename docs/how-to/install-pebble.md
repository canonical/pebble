# How to install Pebble

To install the latest version of Pebble, run the following command (we don't currently ship binaries, so you must first [install Go](https://go.dev/doc/install)):

```
go install github.com/canonical/pebble/cmd/pebble@latest
```

Pebble is invoked using `pebble <command>`. To get more information:

* To see a help summary, type `pebble -h`.
* To see a short description of all commands, type `pebble help --all`.
* To see details for one command, type `pebble help <command>` or `pebble <command> -h`.

A few of the commands that need more explanation are detailed below.
