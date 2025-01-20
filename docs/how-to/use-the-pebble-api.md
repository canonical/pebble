# How to use the Pebble API to manage services

Pebble provides a command-line interface (CLI) for managing services, similar to systemd. However, Pebble distinguishes itself with its dedicated REST API.

This guide demonstrates how to use the Pebble API to programmatically manage services as part of an automated workflow where we can start, test, and stop interdependent services in a CI pipeline. While shell scripts could be used for this purpose, Pebble API offers a more robust approach with easier error handling and reusable components. This reduces manual work, errors, and ensures consistency across environments.

## Using the API

```{include} /reuse/api.md
   :start-after: Start: Pebble API overview
   :end-before: End: Pebble API overview
```

For reference information about the API, see [](../explanation/api-and-clients) and [](../reference/api).

Suppose we start the Pebble daemon with no default services and an empty layer:

```{terminal}
   :input: pebble run
2024-12-30T01:07:10.275Z [pebble] Started daemon.
2024-12-30T01:07:10.281Z [pebble] POST /v1/services 75.042µs 400
2024-12-30T01:07:10.281Z [pebble] Cannot start default services: no default services
```

Here is an example in Python:

```python
import ops

client = ops.pebble.Client("/path/to/.pebble.socket")

# get services
services = client.get_services()
assert len(services) == 0

# add layer
layerYAML = """
services:
  svc1:
    override: replace
    command: sleep 100
"""
client.add_layer(label="ci", layer=layerYAML, combine=True)

# start services
changeID = client.start_services(["svc1"])  # Python client also waits for change to finish

#  get services, the service svc1 should be active
services = client.get_services()
for svc in services:
    print(f"The status of service {svc.name} is: {svc.current}.")

# Now we can run some tests against those services.

# stop services
changeID = client.stop_services(["svc1"])  # Python client also waits for change to finish

# wait for the change
client.wait_change(changeID)
```

You can also use Go or curl to achieve the same result. For more information, see {ref}`api_go_client` and {ref}`api_curl`.

## See more

- [Go client](https://pkg.go.dev/github.com/canonical/pebble/client)
- [Python client for Pebble API](https://ops.readthedocs.io/en/latest/reference/pebble.html)
- [API and clients](../explanation/api-and-clients)
- [API](../reference/api)
