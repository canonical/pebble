
# How to manage identities

Pebble uses named [identities](../reference/identities.md) to extend access to its [API](../explanation/api-and-clients.md).

For example, let's say you're building a container-based system with one container running Pebble and the workload, and another container in the same user namespace talking to Pebble over the API. You may want to give API access to a non-root user -- that's where identities come in.

By default any UID (user ID) connected to the API socket is a `read` user; UID 0 (root) or the UID of the Pebble server process gives `admin` access.


## Add new identities

To extend access to Pebble's API to additional users, add named identities using the [`add-identities`](../reference/cli-commands/add-identities.md) command with a YAML file configuring the details. For example, to add a new admin "bob" with UID 42 and a new read user "alice" with UID 2000, prepare this file:

```yaml
# idents-add.yaml
identities:
    bob:
        access: admin
        local:
            user-id: 42
    alice:
        access: read
        local:
            user-id: 2000
```

and run the following command:

```{terminal}
   :input: pebble add-identities --from idents-add.yaml
Added 2 new identities.
```


## Remove identities

To remove existing identities, use [`remove-identities`](../reference/cli-commands/remove-identities.md)with a YAML file that has a `null` value for each identity you want to remove. For example, to remove "alice", prepare this file:

```yaml
# idents-remove.yaml
identities:
    alice: null
```

and run the following command:

```{terminal}
   :input: pebble remove-identities --from idents-remove.yaml
Removed 1 identity.
```


## Update or replace identities

To update existing identities, use [`update-identities`](../reference/cli-commands/update-identities.md). For example, prepare this file:

```yaml
# idents-update.yaml
identities:
    bob:
        access: admin
        local:
            user-id: 1042
```

and run the following command:

```{terminal}
   :input: pebble update-identities --from idents-update.yaml
Updated 1 identity.
```

You can use the `--replace` flag to idempotently add or update (or even remove) identities, whether or not they exist. The replace option is useful in automated scripts. For example, to update "bob", add "alice", and remove "mallory" (if it exists), prepare this file:

```yaml
# idents-replace.yaml
identities:
    bob:
        access: admin
        local:
            user-id: 1042
    alice:
        access: read
        local:
            user-id: 3000
    mallory: null
```

and run the following command:

```{terminal}
   :input: pebble update-identities --from idents-replace.yaml --replace
Replaced 3 identities.
```


## List identities

You can list identities with the [`identities`](../reference/cli-commands/identities.md) command:

```{terminal}
   :input: pebble identities
Name   Access  Types
alice  read    local
bob    admin   local
```

Use `--format yaml` (or `--format json`) to show all non-secret fields in the YAML (or JSON) format:

```{terminal}
   :input: pebble identities --format=yaml
identities:
    alice:
        access: read
        local:
            user-id: 3000
    bob:
        access: admin
        local:
            user-id: 1042
```


## View a single identity

To show a single identity in YAML format, use the `identity` command:

```{terminal}
   :input: pebble identity alice
access: read
local:
    user-id: 2000
```


## Seed initial identities

To seed a Pebble server with one or more initial identities, use [`pebble run`](../reference/cli-commands/run.md) with the `--identities` option. This has the effect of running `update-identities --replace` with the content of the given identities file before running the server:

```{terminal}
   :input: pebble run --identities idents-add.yaml 
2024-08-12T03:04:51.785Z [pebble] Started daemon.
...
```
