# How to use the Pebble API to manage services

Pebble provides a command-line interface for managing services, similar to systemd. However, Pebble distinguishes itself with its dedicated REST API.

This guide demonstrates how to use the Pebble API to programmatically manage services as part of an automated workflow that starts, tests, and stops interdependent services in a CI pipeline. While shell scripts could be used for this purpose, the Pebble API offers a more robust approach with easier error handling and reusable components. This reduces manual work, errors, and ensures consistency across environments.

## Use the API

Pebble's API allows clients to interact remotely with the daemon. It uses HTTP over a Unix socket, with access controlled by user ID.

For an explanation of API access levels, see [API and clients](/explanation/api-and-clients). For the full API reference, see [API](/reference/api).

Suppose we start the Pebble daemon with no default services and an empty layer:

```{terminal}
   :input: pebble run
2024-12-30T01:07:10.275Z [pebble] Started daemon.
2024-12-30T01:07:10.281Z [pebble] POST /v1/services 75.042µs 400
2024-12-30T01:07:10.281Z [pebble] Cannot start default services: no default services
```

We can then use a Python client to interact with Pebble:

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
client.add_layer("label1", layerYAML, combine=True)

# start services
client.start_services(["svc1"])  # Python client also waits for change to finish

#  get services, the service svc1 should be active
services = client.get_services()
for svc in services:
    print(f"The status of service {svc.name} is: {svc.current}.")

# Now we can run some tests against those services.

# stop services
client.stop_services(["svc1"])  # Python client also waits for change to finish
```

You can also use Go or curl to achieve the same result. For more information, see {ref}`api_go_client` and {ref}`api_curl`.

## See more

- [Go client](https://pkg.go.dev/github.com/canonical/pebble/client)
- [Python client for Pebble API](https://ops.readthedocs.io/en/latest/reference/pebble.html)
- [Explanation of API access control](/explanation/api-and-clients)
- [API reference](/reference/api)
