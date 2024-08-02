(reference_pebble_notice_command)=
# notice command

The `notice` command is used to fetch a single notice.

## Usage

<!-- START AUTOMATED OUTPUT -->
```{terminal}
:input: pebble notice --help
Usage:
  pebble notice [notice-OPTIONS] <id-or-type> [<key>]

The notice command fetches a single notice, either by ID (1-arg variant), or
by unique type and key combination (2-arg variant).

[notice command options]
      --uid=            Look up notice from user with this UID (admin only;
                        2-arg variant only)
```
<!-- END AUTOMATED OUTPUT -->

## Examples

You can fetch a notice either by ID or by type/key combination.

 To fetch the notice with ID "1":

```{terminal}
   :input: pebble notice 1
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

To fetch the notice with type "custom" and key "example.com<span></span>/bar":

```{terminal}
   :input: pebble notice custom other.com/bar
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

Read more: [Notices](../notices.md).
