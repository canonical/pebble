# Identities

Pebble has the concept of named "identities", which lets administrators manage users' access to the [API](../explanation/api-and-clients.md).

Each identity has a name, an access level, and an authentication type with type-specific configuration. Admins use the identities CLI commands to [manage identities](../how-to/manage-identities.md), and the identity configuration is persisted to disk.

The identity configuration must be provided to Pebble, and is read from Pebble, in the following YAML format:

```yaml
identities:
    <name>:
        # (Required) Access level of this identity. Possible values are:
        #
        # - untrusted: has access only to untrusted or "open" endpoints
        # - read: has access to read or "user" endpoints
        # - admin: has access to all endpoints
        access: untrusted | read | admin

        # Configure local, peer credential-based authentication.
        #
        # Currently the only suported authentication type is "local". Other
        # types may be added in future, at which point you may configure an
        # identity with one or more authentication types.
        local:
            # (Required) Peer credential UID.
            user-id: <uid>
```

For example, a local identity named "bob" with UID 42 that is granted `admin` access would be defined as follows:

```yaml
identities:
    bob:
        access: admin
        local:
            user-id: 42
```
