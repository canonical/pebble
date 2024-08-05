# Pebble

_Take control of your internal daemons!_

**Pebble** is an open-source, declarative, client/server service manager that helps you orchestrate a set of local processes as an organised set on UNIX-like operating systems. It resembles well-known tools such as _supervisord_, _runit_, or _s6_, in that it can easily manage non-system processes independently from the system services, but it was designed with unique features that help with more specific use cases.

## Learn the fundamentals

Before harnessing the full potential of Pebble, it is beneficial to grasp the underlying technology that powers Pebble and the ecosystem within which Pebble operates. The following section offers valuable links to deepen your understanding:

- Pebble runs on UNIX-like operating systems:
  - [An Introduction to Linux Basics](https://www.digitalocean.com/community/tutorials/an-introduction-to-linux-basics)
  - [Multipass tutorial: Ubuntu VMs on demand for any workstation](https://multipass.run/docs/tutorial)
  - [How to run an Ubuntu Desktop virtual machine using VirtualBox 7](https://ubuntu.com/tutorials/how-to-run-ubuntu-desktop-on-a-virtual-machine-using-virtualbox#1-overview)
  - [The Linux command line for beginners](https://ubuntu.com/tutorials/command-line-for-beginners#1-overview)
- Pebble uses YAML for configuration:
  - [The Official YAML Web Site](https://yaml.org/)
  - [YAML Tutorial: Everything You Need to Get Started in Minutes](https://www.cloudbees.com/blog/yaml-tutorial-everything-you-need-get-started)
- If you are using Pebble with Juju and Charms:
  - [Juju Documentation](https://juju.is/docs/juju)
  - [Charm SDK Documentation](https://juju.is/docs/sdk)

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
