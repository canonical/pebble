# Getting started

In this tutorial, we will download and install Pebble, configure layers, run the Pebble daemon, and work with layers and services. After this tutorial, you will have a basic understanding of what Pebble is, how to install it, and how to use it to orchestrate services. It takes about 15 minutes to complete.


After that, you can continue exploring more advanced features and use cases (links at the end).

## Prerequisites

- A Linux machine.

## Download and install Pebble

Find the latest tag on the [latest release page](https://github.com/canonical/pebble/releases/latest), then run the following commands to download, extract, and install the latest release (replace `v1.11.0` with the latest tag and `amd64` with your architecture):

```bash
wget https://github.com/canonical/pebble/releases/download/v1.11.0/pebble_v1.11.0_linux_amd64.tar.gz
tar zxvf pebble_v1.11.0_linux_amd64.tar.gz
sudo mv pebble /usr/local/bin/ # make sure it's in your $PATH
```

## Verify Pebble installation

Once the installation is complete, verify that `pebble` has been installed correctly by running:

```bash
pebble
```

This should produce output similar to the following:

```bash
Pebble lets you control services and perform management actions on
the system that is running them.

Usage: pebble <command> [<options>...]

...
```

## Configure Pebble

Now that Pebble has been installed, we can set up a basic configuration.

First, let's create a directory for Pebble configuration:

```bash
mkdir -p ~/PEBBLE/layers
export PEBBLE=$HOME/PEBBLE
echo "export PEBBLE=$HOME/PEBBLE" >> ~/.bashrc # add $PEBBLE to your bashrc
```

Next, create a layer by running:

```bash
echo """\
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

## Start the Pebble daemon

Now we are ready to run the Pebble daemon.

Pebble is invoked using `pebble <command>`. (To get more information, run `pebble -h`.)

To start the daemon, run:

```bash
pebble run
```

This starts the Pebble daemon itself, as well as all the services that are marked as `startup: enabled` (in our simple layer created above, the `http-server` service is marked as `startup: enabled`). You should get some output similar to the following:

```bash
2024-06-02T11:30:02.925Z [pebble] Started daemon.
2024-06-02T11:30:02.936Z [pebble] POST /v1/services 10.751704ms 202
2024-06-02T11:30:02.936Z [pebble] Started default services with change 77.
2024-06-02T11:30:02.942Z [pebble] Service "http-server" starting: python3 -m http.server 8080
...
```

As you can see from the log, our HTTP server is started too, which can be verified by running `curl localhost:8080` in another terminal tab.

> Note: To exit the Pebble daemon, press Ctrl-C (which sends an "interrupt" signal to the process).

## View, start and stop services

While the Pebble daemon is running, you can view the status of services by running `pebble services`. Open another terminal tab, and run:

```bash
pebble services
```

You should see output similar to the following:

```bash
Service      Startup  Current  Since
http-server  enabled  active   today at 11:30 UTC
```

Use `pebble stop <service1> <service2> ...` to stop one or more services. You can stop the running `http-server` service by running:

```bash
pebble stop http-server
```

You should get output similar to the following:

```bash
Service      Startup  Current   Since
http-server  enabled  inactive  today at 11:33 UTC
```

Now the service `http-server` is stopped. If we run `curl localhost:8080` again, we get a "connection refused" error, which confirms the service is down.

To start it again, run:

```bash
pebble start http-server
```

## Add a new layer

Now let's add another layer containing a different service that is also an HTTP server. To create a new layer, run:

```bash
echo """\
summary: Simple layer 2

description: |
    Yet another simple layer.

services:
    http-server-2:
        override: replace
        summary: demo http server 2
        command: python3 -m http.server 8081
        startup: enabled
""" > $PEBBLE/layers/002-another-http-server.yaml
```

This creates another layer that also contains a single service (running a basic
HTTP server using the Python `http.server` module listening on a different port 8081).

Add the new layer to a Pebble plan by running:

```bash
pebble add layer1 $PEBBLE/layers/002-another-http-server.yaml
```

If the layer is added successfully, the above command should produce the following output:

```bash
Layer "layer1" added successfully from "/home/ubuntu/PEBBLE_HOME/layers/002-another-http-server.yaml"
```

Even though the service configuration has been updated with the newly added layer, the newly added services won't be automatically started. If we check the services:

```bash
pebble services
```
We can see that although the new service `http-server-2` has been added, it's still "inactive":

```bash
Service        Startup  Current   Since
http-server    enabled  active    today at 11:41 UTC
http-server-2  enabled  inactive  -
```

To bring the service state in sync with the new configuration, run `pebble replan`:

```bash
pebble replan
```

And you get output similar to:

```bash
2024-06-02T11:40:39Z INFO Service "http-server" already started.
```

Now if we check all the services again:

```bash
pebble services
```

We can see that the new HTTP server `http-server-2` defined in the newly added layer should have been started and be shown as "active":

```bash
Service        Startup  Current  Since
http-server    enabled  active   today at 11:34 UTC
http-server-2  enabled  active   today at 11:40 UTC
```

## Next steps

- To learn more about running the Pebble daemon, see [How to run the daemon (server)](../how-to/run-the-daemon.md).
- To learn more about viewing, starting and stopping services, see [How to view, start, and stop services](../how-to/view-start-stop-services.md).
- To learn more about updating and restarting services, see [How to update and restart services](../how-to/update-restart-services.md).
- To learn more about configuring layers, see [How to configure layers](../how-to/configure-layers.md).
- To learn more about layer configuration options, read the [Layer specification](../reference/layer-specification.md).
