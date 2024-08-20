# Notices

Pebble includes a subsystem called *notices*, which allows the user to introspect various events that occur in the Pebble server, as well as record custom client events.

## How it works

The server saves notices to disk, so they persist across restarts, and expire after a notice-defined interval.

Each notice is either public or has a specific user ID. Public notices may be viewed by any user, while notices that have a user ID may only be viewed by users with that same user ID, or by an admin (root, or the user the Pebble daemon is running as).

Each notice is uniquely identified by its *user ID*, *type* and *key* combination, and the notice's count of occurrences is incremented every time a notice with that type and key combination occurs.

Each notice records the time it first occurred, the time it last occurred, and the time it last repeated.

A *repeat* happens when a notice occurs with the same user ID, type, and key as a prior notice, and either the notice has no "repeat after" duration (the default), or the notice happens after the provided "repeat after" interval (since the prior notice). Thus, specifying "repeat after" prevents a notice from appearing again if it happens more frequently than desired.

In addition, a notice records optional *data* (string key-value pairs) from the last occurrence.

## Notice types

These notice types are currently available:

* `change-update`: recorded whenever a change is first spawned or its status is updated. The key for this type of notice is the change ID, and the notice's data includes the change `kind`.

* `custom`: a custom client notice reported via `pebble notify`. The key and any data is provided by the user. The key must be in the format `example.com/path` to ensure well-namespaced notice keys.

* `warning`: Pebble warnings are implemented in terms of notices. The key for this type of notice is the human-readable warning message.

## Commands

- {ref}`reference_pebble_notice_command`
- {ref}`reference_pebble_notices_command`
- {ref}`reference_pebble_notify_command`
