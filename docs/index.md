# Pebble

**Pebble** is a lightweight Linux service manager.

It helps you orchestrate a set of local processes as an organised set. It resembles well-known tools such as _supervisord_, _runit_, or _s6_, in that it can easily manage non-system processes independently from the system services. However, it was designed with unique features such as layered configuration and an HTTP API that help with more specific use cases.

Pebble fulfils the need for streamlined, dependable, and secure service management. It empowers you to efficiently operate your services with ease, resolve service interdependencies, configure tailored health checks to automatically restart services upon failure, and gain insightful visibility into Pebble server events for a comprehensive understanding of operational dynamics. Pebble also supports simple identity and access control, ensuring secure usage in shared environments.

Pebble is useful for developers who are building [Juju charms on Kubernetes](https://juju.is/docs/sdk/from-zero-to-hero-write-your-first-kubernetes-charm), creating [Rocks](https://documentation.ubuntu.com/rockcraft/en/latest/explanation/rocks/) or Docker images, or orchestrating services in the virtual machine.

## In this documentation

````{grid} 1 1 2 2
```{grid-item-card} [Tutorial](tutorial/getting-started)
**Start here**: a hands-on getting started introduction to Pebble for new users.
```

```{grid-item-card} [How-to guides](how-to/index)
**Step-by-step guides** covering key operations and common tasks.
```
````

````{grid} 1 1 2 2
```{grid-item-card} [Explanation](explanation/index)
**Discussion and clarification** of key topics.
```

```{grid-item-card} [Reference](reference/index)
**Technical information** - specifications, APIs, and architecture.
```
````

## Project and community

Pebble is free software and released under [GPL-3.0](https://www.gnu.org/licenses/gpl-3.0.en.html).

The Pebble project is sponsored by [Canonical Ltd](https://www.canonical.com).

- [Code of conduct](https://ubuntu.com/community/ethos/code-of-conduct).
- [Contribute to the project](https://github.com/canonical/pebble?tab=readme-ov-file#contributing)
- [Development](https://github.com/canonical/pebble/blob/master/HACKING.md): information on how to run and hack on the Pebble codebase during development.

```{filtered-toctree}
:hidden:
:titlesonly:

:diataxis:Tutorial <tutorial/getting-started>
:diataxis:how-to/index
:diataxis:explanation/index
:diataxis:reference/index
```
