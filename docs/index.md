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

````{grid} 1 1 2 2
```{grid-item-card} [Tutorial](tutorial/getting-started)
**Start here**: a hands-on introduction to Pebble, guiding you through your first steps using the CLI
```

```{grid-item-card} [How-to guides](how-to/index)
**Step-by-step guides** covering key operations and common tasks
- [Install Pebble](how-to/install-pebble)
- [Manage service dependencies](how-to/service-dependencies)
- [Manage identities](how-to/manage-identities)
```
````

````{grid} 1 1 2 2
:reverse:
```{grid-item-card} [Reference](reference/index)
**Technical information**
- [Layer specification](reference/layer-specification)
- [CLI commands](reference/cli-commands)
```

```{grid-item-card} [Explanation](explanation/index)
**Discussion and clarification** of key topics
- [General model](explanation/general-model.md)
- [API and clients](explanation/api-and-clients.md)
```
````

## Releases

[Read the release notes](https://github.com/canonical/pebble/releases)

Pebble releases are tracked in GitHub. To get notified when there's a new release, watch the [Pebble repository](https://github.com/canonical/pebble).

## Project and community

Pebble is free software and released under [GPL-3.0](https://www.gnu.org/licenses/gpl-3.0.en.html).

The Pebble project is sponsored by [Canonical Ltd](https://canonical.com).

- [Code of conduct](https://ubuntu.com/community/docs/ethos/code-of-conduct)
- [Contribute to the project](https://github.com/canonical/pebble?tab=readme-ov-file#contributing)
- [Development](https://github.com/canonical/pebble/blob/master/HACKING.md): information on how to run and hack on the Pebble codebase during development
