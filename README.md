# The Pebble service manager

[![pebble](https://snapcraft.io/pebble/badge.svg)](https://snapcraft.io/pebble)
[![snap](https://github.com/canonical/pebble/actions/workflows/snap.yml/badge.svg)](https://github.com/canonical/pebble/actions/workflows/snap.yml)
[![binaries](https://github.com/canonical/pebble/actions/workflows/binaries.yml/badge.svg)](https://github.com/canonical/pebble/actions/workflows/binaries.yml)
[![tests](https://github.com/canonical/pebble/actions/workflows/tests.yml/badge.svg)](https://github.com/canonical/pebble/actions/workflows/tests.yml)

_Take control of your internal daemons!_

**Pebble** is a lightweight Linux service manager that helps you orchestrate a set of local processes as an organised set. It resembles well-known tools such as _supervisord_, _runit_, or _s6_, in that it can easily manage non-system processes independently from the system services. However, it was designed with unique features such as layered configuration and an HTTP API that help with more specific use cases.

Pebble's key features:

- [Layer](https://canonical-pebble.readthedocs-hosted.com/en/latest/reference/layer-specification/)-based configuration
- Service [dependencies](https://canonical-pebble.readthedocs-hosted.com/en/latest/explanation/service-dependencies/)
- Service [logs](https://canonical-pebble.readthedocs-hosted.com/en/latest/reference/cli-commands/#logs) and [log forwarding](https://canonical-pebble.readthedocs-hosted.com/en/latest/reference/log-forwarding/)
- [Health checks](https://canonical-pebble.readthedocs-hosted.com/en/latest/reference/health-checks/)
- [Notices](https://canonical-pebble.readthedocs-hosted.com/en/latest/reference/notices/) (aggregated events)
- [Identities](https://canonical-pebble.readthedocs-hosted.com/en/latest/how-to/manage-identities/)
- Can be used in [virtual machines and containers](https://canonical-pebble.readthedocs-hosted.com/en/latest/how-to/manage-a-remote-system/)
- [CLI commands](https://canonical-pebble.readthedocs-hosted.com/en/latest/reference/cli-commands/)
- [HTTP API](https://canonical-pebble.readthedocs-hosted.com/en/latest/explanation/api-and-clients/) with a [Go client](https://pkg.go.dev/github.com/canonical/pebble/client) and a [Python client](https://github.com/canonical/operator/blob/main/ops/pebble.py)

## Quick start

At any Linux shell:

```bash
go install github.com/canonical/pebble/cmd/pebble@latest
mkdir -p ~/.config/pebble/layers
export PEBBLE=$HOME/.config/pebble

echo """\
services:
    demo-service:
        override: replace
        command: sleep 1000
        startup: enabled
""" > $PEBBLE/layers/001-demo-service.yaml

pebble run
```

Read more about Pebble's general model [here](https://canonical-pebble.readthedocs-hosted.com/en/latest/explanation/general-model/).

For a hands-on introduction to Pebble, we recommend going through the [tutorial](https://canonical-pebble.readthedocs-hosted.com/en/latest/tutorial/getting-started/).

## Getting help

To get the most out of Pebble, we recommend starting with the [documentation](https://canonical-pebble.readthedocs-hosted.com/en/latest/).

You can [create an issue](https://github.com/canonical/pebble/issues/new) and we will help!

## Hacking and development

See [HACKING.md](HACKING.md) for information on how to run and hack on the Pebble codebase during development. In short, use `go run ./cmd/pebble`.

## Contributing

We welcome quality external contributions. We have good unit tests for much of the code, and a thorough code review process. Please note that unless it's a trivial fix, it's generally worth opening an issue to discuss before submitting a pull request.

Before you contribute a pull request you should sign the [Canonical contributor agreement](https://ubuntu.com/legal/contributors) -- it's the easiest way for you to give us permission to use your contributions.
