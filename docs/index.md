# Pebble

```{toctree}
:maxdepth: 2
:hidden: true

Tutorial <tutorial/getting-started>
how-to/index
reference/index
explanation/index
```

**Pebble** is a lightweight Linux service manager.

It helps you orchestrate a set of local processes as an organized set. It resembles well-known tools such as _supervisord_, _runit_, or _s6_, in that it can easily manage non-system processes independently from the system services. However, it was designed with unique features such as layered configuration and an HTTP API that help with more specific use cases.

If you need a way to manage one or more services in a container, or as a non-root user on a machine, Pebble might be for you. It handles service logs, service dependencies, and allows you to set up ongoing health checks. Plus, it has an "HTTP over Unix socket" API for all operations, with simple UID-based access control.

Pebble is useful for developers who are building {external+operator:ref}`Juju charms on Kubernetes <from-zero-to-hero-write-your-first-kubernetes-charm>`, creating {external+rockcraft:ref}`Rock <explanation-rocks>` or Docker images, or orchestrating services in the virtual machine.

## In this documentation

### Get started

{doc}`Tutorial <tutorial/getting-started>` — A hands-on introduction to Pebble

### Use Pebble

````{domain} Use Pebble
```{slice} CLI
{doc}`Overview <explanation/general-model>` slice
{doc}`Install <how-to/install-pebble>`
{doc}`Manage a remote system <how-to/manage-a-remote-system>`
{doc}`Environment variables <reference/environment-variables>`
{doc}`CLI commands <reference/cli-commands>`
```

```{slice} Services
{doc}`Start order <explanation/service-start-order>`
{doc}`Service dependencies <explanation/service-dependencies>`
{doc}`Manage service dependencies <how-to/service-dependencies>`
{doc}`Auto-restart <reference/service-auto-restart>`
```

```{slice} HTTP API
{doc}`Overview <explanation/api-and-clients>` slice
{doc}`Use the API to manage services <how-to/use-the-pebble-api>`
{doc}`Reference <reference/api>` slice
```

```{slice} Layers
{doc}`Use layers <how-to/use-layers>`
{doc}`Specification <reference/layer-specification>`
```

```{slice} Logging
{doc}`Forward logs to Loki <how-to/forward-logs-to-loki>`
{doc}`Capture logs from files <how-to/capture-logs-from-files>`
{doc}`Reference <reference/log-forwarding>` slice
```

```{slice} Health checks
{doc}`Run services reliably <how-to/run-services-reliably>`
{doc}`Check health based on logs <how-to/check-health-based-on-logs>`
{doc}`Reference <reference/health-checks>` slice
```

```{slice} Reporting
{doc}`Changes and tasks <reference/changes-and-tasks>`
{doc}`Notices <reference/notices>`
```

```{slice} Access
{doc}`Manage identities <how-to/manage-identities>`
{doc}`Reference <reference/identities>` slice
```

```{slice} Security
{doc}`Overview <explanation/security>` slice
```
````

## How this documentation is organised

This documentation uses the [Diátaxis documentation structure](https://diataxis.fr/).

- {doc}`Tutorials <tutorial/getting-started>`
- {doc}`How-to guides <how-to/index>`
- {doc}`Reference <reference/index>`
- {doc}`Explanation <explanation/index>`

## Releases

[Read the release notes](https://github.com/canonical/pebble/releases)

Pebble releases are tracked in GitHub. To get notified when there's a new release, watch the [Pebble repository](https://github.com/canonical/pebble).

## Project and community

Pebble is free software and released under [GPL-3.0](https://www.gnu.org/licenses/gpl-3.0.en.html).

The Pebble project is sponsored by [Canonical Ltd](https://canonical.com).

- [Code of conduct](https://ubuntu.com/community/docs/ethos/code-of-conduct)
- [Contribute to the project](https://github.com/canonical/pebble?tab=readme-ov-file#contributing)
- [Development](https://github.com/canonical/pebble/blob/master/HACKING.md): information on how to run and hack on the Pebble codebase during development
