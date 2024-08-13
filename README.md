# The Pebble service manager

[![pebble](https://snapcraft.io/pebble/badge.svg)](https://snapcraft.io/pebble)
[![snap](https://github.com/canonical/pebble/actions/workflows/snap.yml/badge.svg)](https://github.com/canonical/pebble/actions/workflows/snap.yml)
[![binaries](https://github.com/canonical/pebble/actions/workflows/binaries.yml/badge.svg)](https://github.com/canonical/pebble/actions/workflows/binaries.yml)
[![tests](https://github.com/canonical/pebble/actions/workflows/tests.yml/badge.svg)](https://github.com/canonical/pebble/actions/workflows/tests.yml)

_Take control of your internal daemons!_

**Pebble** is a lightweight Linux service manager that helps you orchestrate a set of local service processes as an organized set. It resembles well known tools such as _supervisord_, _runit_, or _s6_, in that it can easily manage non-system processes independently from the system services, but it was designed with unique features that help with more specific use cases.

Pebble's key features:

- Service management with [layers](https://canonical-pebble.readthedocs-hosted.com/en/latest/reference/layers/)
- Service [dependencies](https://canonical-pebble.readthedocs-hosted.com/en/latest/explanation/service-dependencies/)
- Service [logs](https://canonical-pebble.readthedocs-hosted.com/en/latest/reference/cli-commands/logs/) and [log forwarding](https://canonical-pebble.readthedocs-hosted.com/en/latest/reference/log-forwarding/)
- [Health checks](https://canonical-pebble.readthedocs-hosted.com/en/latest/reference/health-checks/)
- [Notices](https://canonical-pebble.readthedocs-hosted.com/en/latest/reference/notices/)
- Identities
- Can be used in Machines and containers
- [CLI](https://canonical-pebble.readthedocs-hosted.com/en/latest/reference/cli-commands/cli-commands/), [HTTP API and Go client](https://canonical-pebble.readthedocs-hosted.com/en/latest/explanation/api-and-clients/)

## Quick start

Prerequisites:

- A Linux machine.
- Python 3.x (used to run a basic HTTP server as a sample service managed by Pebble).

```bash
git clone https://github.com/canonical/pebble.git
cd pebble
mkdir -p ~/PEBBLE/layers
export PEBBLE=$HOME/PEBBLE
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
go run ./cmd/pebble run
```

You can also follow the [Getting started with Pebble tutorial](https://canonical-pebble.readthedocs-hosted.com/en/latest/tutorial/getting-started/).

## Getting help

If you need support, start with the [documentation](https://canonical-pebble.readthedocs-hosted.com/en/latest/).

To learn more about Pebble, read the following sections:

- [General model](https://canonical-pebble.readthedocs-hosted.com/en/latest/explanation/general-model/)
- [Layer configuration examples](https://canonical-pebble.readthedocs-hosted.com/en/latest/reference/layers/)
- [Container usage](https://canonical-pebble.readthedocs-hosted.com/en/latest/reference/pebble-in-containers/)
- [Layer specification](https://canonical-pebble.readthedocs-hosted.com/en/latest/reference/layer-specification/)
- [API and clients](https://canonical-pebble.readthedocs-hosted.com/en/latest/explanation/api-and-clients/)
- [Hacking / Development](#hacking--development)
- [Contributing](#contributing)

## Hacking / development

See [HACKING.md](HACKING.md) for information on how to run and hack on the Pebble codebase during development. In short, use `go run ./cmd/pebble`.

## Contributing

We welcome quality external contributions. We have good unit tests for much of the code, and a thorough code review process. Please note that unless it's a trivial fix, it's generally worth opening an issue to discuss before submitting a pull request.

Before you contribute a pull request you should sign the [Canonical contributor agreement](https://ubuntu.com/legal/contributors) -- it's the easiest way for you to give us permission to use your contributions.

## Have fun!

... and enjoy the rest of the year!
