# How to Config Layers

Below is an example of the current configuration format. For full details of all fields, see the [complete layer specification](../reference/layer-specification).

```yaml
summary: Simple layer

description: |
    A better description for a simple layer.

services:
    srv1:
        override: replace
        summary: Service summary
        command: cmd arg1 "arg2a arg2b"
        startup: enabled
        after:
            - srv2
        before:
            - srv3
        requires:
            - srv2
            - srv3
        environment:
            VAR1: val1
            VAR2: val2
            VAR3: val3

    srv2:
        override: replace
        startup: enabled
        command: cmd
        before:
            - srv3

    srv3:
        override: replace
        command: cmd
```

The `override` field (which is required) defines whether this entry _overrides_ the previous service of the same name (if any), or merges with it. See the [full layer specification](../reference/layer-specification) for more details.

---

## Layer Override Example

Any of the fields can be replaced individually in a merged service configuration. To illustrate, here is a sample override layer that might sit on top of the one above:

```yaml
summary: Simple override layer

services:
    srv1:
        override: merge
        environment:
            VAR3: val3
        after:
            - srv4
        before:
            - srv5

    srv2:
        override: replace
        summary: Replaced service
        startup: disabled
        command: cmd

    srv4:
        override: replace
        command: cmd
        startup: enabled

    srv5:
        override: replace
        command: cmd
```
