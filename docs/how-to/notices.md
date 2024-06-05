# How to use notices

Pebble includes a subsystem called *notices*, which allows the user to introspect various events that occur in the Pebble server, as well as record custom client events. The server saves notices to disk, so they persist across restarts, and expire after a notice-defined interval.

Each notice is either public or has a specific user ID. Public notices may be viewed by any user, while notices that have a user ID may only be viewed by users with that same user ID, or by an admin (root, or the user the Pebble daemon is running as).

Each notice is uniquely identified by its *user ID*, *type* and *key* combination, and the notice's count of occurrences is incremented every time a notice with that type and key combination occurs.

Each notice records the time it first occurred, the time it last occurred, and the time it last repeated.

A *repeat* happens when a notice occurs with the same user ID, type, and key as a prior notice, and either the notice has no "repeat after" duration (the default), or the notice happens after the provided "repeat after" interval (since the prior notice). Thus, specifying "repeat after" prevents a notice from appearing again if it happens more frequently than desired.

In addition, a notice records optional *data* (string key-value pairs) from the last occurrence.

These notice types are currently available:

<!-- TODO: * `change-update`: recorded whenever a change is first spawned or its status is updated. The key for this type of notice is the change ID, and the notice's data includes the change `kind`. -->

* `custom`: a custom client notice reported via `pebble notify`. The key and any data is provided by the user. The key must be in the format `example.com/path` to ensure well-namespaced notice keys.

<!-- TODO: * `warning`: Pebble warnings are implemented in terms of notices. The key for this type of notice is the human-readable warning message.

See comment at the top of internals/overlord/state/warning.go for more info.
-->

To record `custom` notices, use `pebble notify` -- the notice user ID will be set to the client's user ID:

```
$ pebble notify example.com/foo
Recorded notice 1
$ pebble notify example.com/foo
Recorded notice 1
$ pebble notify other.com/bar name=value email=john@smith.com  # two data fields
Recorded notice 2
$ pebble notify example.com/foo
Recorded notice 1
```

The `pebble notices` command lists notices not yet acknowledged, ordered by the last-repeated time (oldest first). After it runs, the notices that were shown may then be acknowledged by running `pebble okay`. When a notice repeats (see above), it needs to be acknowledged again.

```
$ pebble notices
ID   User    Type    Key              First                Repeated             Occurrences
1    1000    custom  example.com/foo  today at 16:16 NZST  today at 16:16 NZST  3
2    public  custom  other.com/bar    today at 16:16 NZST  today at 16:16 NZST  1
```

To fetch details about a single notice, use `pebble notice`, which displays the output in YAML format. You can fetch a notice either by ID or by type/key combination.

To fetch the notice with ID "1":

```
$ pebble notice 1
id: "1"
user-id: 1000
type: custom
key: example.com/foo
first-occurred: 2023-09-15T04:16:09.179395298Z
last-occurred: 2023-09-15T04:16:19.487035209Z
last-repeated: 2023-09-15T04:16:09.179395298Z
occurrences: 3
expire-after: 168h0m0s
```

To fetch the notice with type "custom" and key "other.com/bar":

```
$ pebble notice custom other.com/bar
id: "2"
user-id: public
type: custom
key: other.com/bar
first-occurred: 2023-09-15T04:16:17.180049768Z
last-occurred: 2023-09-15T04:16:17.180049768Z
last-repeated: 2023-09-15T04:16:17.180049768Z
occurrences: 1
last-data:
    name: value
    email: john@smith.com
expire-after: 168h0m0s
```
