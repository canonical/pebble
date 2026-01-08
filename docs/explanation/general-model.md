# General model

Pebble is organized as a single binary that works as a daemon and also as a client to itself. When the daemon runs it loads its own configuration from the `$PEBBLE` directory, as defined in the environment, and also records in that same directory its state and Unix sockets for communication. If that variable is not defined, Pebble will attempt to look for its configuration from a default system-level setup at `/var/lib/pebble/default`. Using that directory is encouraged for whole-system setup such as when using Pebble to control services in a container.

The `$PEBBLE` directory must contain a `layers/` subdirectory that holds a stack of configuration files with names similar to `001-base-layer.yaml`, where the digits define the order of the layer and the following label uniquely identifies it. Each layer in the stack sits above the former one, and has the chance to improve or redefine the service configuration as desired.
