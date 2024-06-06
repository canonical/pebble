# Getting started

## 1 Prerequisites

- A Linux machine.

## 2 Install Pebble

### 2.1 Download and install the latest release

Find the latest tag on the [latest release page](https://github.com/canonical/pebble/releases/latest), then run the following commands to download, extract, and install the latest release (replace `v1.11.0` with the latest tag and `amd64` with your architecture):

```bash
$ wget https://github.com/canonical/pebble/releases/download/v1.11.0/pebble_v1.11.0_linux_amd64.tar.gz
$ tar zxvf pebble_v1.11.0_linux_amd64.tar.gz
$ sudo mv pebble /usr/local/bin/ # make sure it's in your $PATH
```

### 2.2 Install from source

Alternatively, you can build and install Pebble from source:

1. Follow the official Go documentation [here](https://go.dev/doc/install) to download and install Go.
2. After installing, you will want to add the `$GOBIN` directory to your `$PATH` so you can use the installed tools. For more information, refer to the [official documentation](https://go.dev/doc/install/source#environment).
3. Run `go install github.com/canonical/pebble/cmd/pebble@latest` to build and install Pebble.

### 2.3 Check that it's working

After installation, if you run `pebble`, you should see some output equivalent to the following:

```bash
$ pebble
Pebble lets you control services and perform management actions on
the system that is running them.

Usage: pebble <command> [<options>...]

...
```

Pebble is invoked using `pebble <command>`. To get more information:

- To see a help summary, type `pebble -h`.
- To see a short description of all commands, type `pebble help --all`.
- To see details for one command, type `pebble help <command>`.

## 3 Configure Pebble

First, create a directory for Pebble configuration:

```bash
$ mkdir -p ~/PEBBLE/layers
$ export PEBBLE=$HOME/PEBBLE
$ echo "export PEBBLE=$HOME/PEBBLE" >> ~/.bashrc # add $PEBBLE to your bashrc
```

Then, create a simple layer:

```bash
$ echo """\
summary: Simple layer

description: |
    A simple layer.

services:
    http-server:
        override: replace
        summary: demo http server
        command: python3 -m http.server 8080
        startup: enabled
""" > $PEBBLE/layers/001-http-server.yaml
```

This creates a simple layer containing only one service (which runs a basic HTTP server using Python's `http` module, listening on port 8080).

## 4 Run the Pebble daemon

```bash
$ pebble run
2024-06-02T11:30:02.925Z [pebble] Started daemon.
2024-06-02T11:30:02.936Z [pebble] POST /v1/services 10.751704ms 202
2024-06-02T11:30:02.936Z [pebble] Started default services with change 77.
2024-06-02T11:30:02.942Z [pebble] Service "http-server" starting: python3 -m http.server 8080
```

This starts the Pebble daemon, and as you can see from the log, our HTTP server is already started, which can be verified by running `curl localhost:8080` in another terminal tab.

To exit the Pebble daemon, press Ctrl-C (which sends an "interrupt" signal to the process).

## 5 View, start, and stop services

You can view the status of services by running `pebble services`. Open another terminal tab, and run:

```bash
$ pebble services
Service      Startup  Current  Since
http-server  enabled  active   today at 11:30 UTC
```

Use `pebble stop <service1> <service2> ...` to stop one or more services. Run:

```bash
$ pebble stop http-server
```

And the service `http-server` is stopped. We can verify it by viewing all the services:

```bash
$ pebble services
Service      Startup  Current   Since
http-server  enabled  inactive  today at 11:33 UTC
```

And, if we run `curl localhost:8080`, we get a "connection refused" error, which confirms the service is down.

To start it again, run:

```bash
$ pebble start http-server
```

And it's started now.

## 6 Add a new layer

Now let's add another layer containing a different HTTP server. Create a new layer:

```bash
$ echo """\
summary: Simple layer 2

description: |
    Yet another simple layer.

services:
    http-server-2:
        override: replace
        summary: demo http server 2
        command: python3 -m http.server 8081
        startup: enabled
""" > $PEBBLE/layers/002-another-server.yaml
```

This creates another layer containing only one service running an HTTP server listening on a different port 8081. Then let's add this layer to a plan:

```bash
$ pebble add layer1 $PEBBLE/layers/002-another-server.yaml
Layer "layer1" added successfully from "/home/ubuntu/PEBBLE_HOME/layers/002-another-server.yaml"
```

When we update the service configuration by adding a layer, the services changed won't be automatically restarted. If we check the services:

```bash
$ pebble services
Service        Startup  Current   Since
http-server    enabled  active    today at 11:41 UTC
http-server-2  enabled  inactive  -
```

We can see that although a new service has been added, it's not active yet.

To bring the service state in sync with the new configuration, run `pebble replan`:

```
$ pebble replan
2024-06-02T11:40:39Z INFO Service "http-server" already started.
```

Now if we check all the services:

```bash
$ pebble services
Service        Startup  Current  Since
http-server    enabled  active   today at 11:34 UTC
http-server-2  enabled  active   today at 11:40 UTC
```

We can see that the new HTTP server defined in the newly added layer has been started as well.

## 7 Where to go from here

- To learn more about running the Pebble daemon, see [How to run the daemon (server)](../how-to/run-the-daemon.md).
- To learn more about viewing, starting and stopping services, see [How to view, start, and stop services](../how-to/view-start-stop-services.md).
- To learn more about updating and restarting services, see [How to update and restart services](../how-to/update-restart-services.md).
- To learn more about configuring layers, see [How to configure layers](../how-to/configure-layers.md).
- To learn more about layer configuration options, read the [Layer specification](../reference/layer-specification.md).
