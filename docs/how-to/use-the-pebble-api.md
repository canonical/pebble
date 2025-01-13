# How to use the Pebble API to manage services

Pebble provides a command-line interface (CLI) for managing services, similar to systemd. However, Pebble distinguishes itself with its dedicated REST API.

This guide demonstrates how to use the Pebble API to programmatically manage services as part of an automated workflow.

## API and automated workflows

As an example scenario, consider an automated workflow that starts and tests a group of inter-dependent services as part of a continuous integration pipeline. The tests could include verifying that the services are running, querying their status and health, and running integration tests. After testing the services, the workflow stops the services.

Although you could use shell scripts and CLI commands to implement the workflow, the Pebble API enables a more robust and maintainable approach:

- You don't need to parse command output.
- You can handle errors in a more sophisticated way.
- You can encapsulate service operations within reusable functions and modules.

This approach reduces manual intervention and the risk of human error. It also supports consistency and idempotency across different environments.

## Using the API
```{include} /reuse/api.md
   :start-after: Start: Pebble API overview
   :end-before: End: Pebble API overview
```
For reference information about the API, see [](../explanation/api-and-clients) and [](../reference/api).

### curl

Most API endpoints are only available via the Unix socket. With curl, there is a `--unix-socket <path>` parameter, which allows us to connect through this Unix socket instead of using the network. In this section, let's see how we can use curl to access the API via the Unix socket.

Suppose we start the Pebble daemon with no default services and an empty layer:

```{terminal}
   :input: pebble run
2024-12-30T01:07:10.275Z [pebble] Started daemon.
2024-12-30T01:07:10.281Z [pebble] POST /v1/services 75.042µs 400
2024-12-30T01:07:10.281Z [pebble] Cannot start default services: no default services
```

To get the services, run: 

```bash
curl --unix-socket $PEBBLE/.pebble.socket -XGET http://localhost/v1/services
```

And we should get the following JSON response showing that there is no service:

```json
{"type":"sync","status-code":200,"status":"OK","result":[]}
```

To add a simple layer with one service, run:

```bash
curl --unix-socket $PEBBLE/.pebble.socket -XPOST http://localhost/v1/layers -d '{"action": "add", "combine": true, "inner": true, "label": "ci", "format": "yaml", "layer": "summary: ci\nservices:\n  svc1:\n    override: replace\n    command: sleep 100\n"}'
```

If we get the services again by running `curl --unix-socket $PEBBLE/.pebble.socket -XGET http://localhost/v1/services`, we will get a different response showing we have a service now but it's not started:

```json
{
  "type": "sync",
  "status-code": 200,
  "status": "OK",
  "result": [
    {
      "name": "svc1",
      "startup": "disabled",
      "current": "inactive"
    }
  ]
}
```

To start the newly added service, run:

```bash
curl --unix-socket $PEBBLE/.pebble.socket -XPOST http://localhost/v1/services -d '{"action": "start", "services": ["svc1"]}'
```

This endpoint is of type "async", returning a change ID instead:

```json
{"type":"async","status-code":202,"status":"Accepted","change":"1","result":null}
```

To wait for the change to be finished, run:

```bash
curl --unix-socket $PEBBLE/.pebble.socket -XGET http://localhost/v1/changes/1/wait
```

And we shall get a response similar to the following:

```json
{
  "type": "sync",
  "status-code": 200,
  "status": "OK",
  "result": {
    "id": "1",
    "kind": "start",
    "summary": "Start service \"svc1\"",
    "status": "Done",
    "tasks": [
      {
        "id": "1",
        "kind": "start",
        "summary": "Start service \"svc1\"",
        "status": "Done",
        "progress": {
          "label": "",
          "done": 1,
          "total": 1
        },
        "spawn-time": "2024-12-30T08:57:43.194037353+08:00",
        "ready-time": "2024-12-30T08:57:44.21087777+08:00"
      }
    ],
    "ready": true,
    "spawn-time": "2024-12-30T08:57:43.194065853+08:00",
    "ready-time": "2024-12-30T08:57:44.210879687+08:00"
  }
}
```

If we try to get the services again, we can see it's active now.

We can integrate these steps in our automated workflow to start services and their dependencies. Then we're able to run some tests against the services. As a final step in the workflow, we'll want to stop the services.

To stop a service, run:

```bash
curl --unix-socket $PEBBLE/.pebble.socket -XPOST http://localhost/v1/services -d '{"action": "stop", "services": ["svc1"]}'
```

This endpoint is `async`, so we'll get a response similar to:

```json
{"type":"async","status-code":202,"status":"Accepted","change":"2","result":null}
```

We can run the same command above to wait for the change to be finished and verify the status by getting the services.

### Go client

We can also use the Go client to achieve the same result:

```go
package main

import (
	"fmt"

	"github.com/canonical/pebble/client"
)

func main() {
	pebble, err := client.New(&client.Config{Socket: "/home/ubuntu/PEBBLE_HOME/.pebble.socket"})
	if err != nil {
		panic(err)
	}

	// get services
	_, err = pebble.Services(&client.ServicesOptions{})
	if err != nil {
		panic(err)
	}

	// add layer
	layerYAML := `
services:
  svc1:
    override: replace
    command: sleep 100
`
	err = pebble.AddLayer(&client.AddLayerOptions{
		Combine:   true,
		Label:     "ci",
		LayerData: []byte(layerYAML),
	})
	if err != nil {
		panic(err)
	}

	// start services
	changeID, err := pebble.Start(&client.ServiceOptions{Names: []string{"svc1"}})
	if err != nil {
		panic(err)
	}

	// wait for the change
	_, err = pebble.WaitChange(changeID, &client.WaitChangeOptions{})
	if err != nil {
		panic(err)
	}

	// get services, the service svc1 should be active
	services, err := pebble.Services(&client.ServicesOptions{})
	if err != nil {
		panic(err)
	}
	for _, svc := range services {
		fmt.Printf("The status of service %s is: %s.\n", svc.Name, svc.Current)
	}

	// Now we can run some tests against those services.

	// stop services
	_, err = pebble.Stop(&client.ServiceOptions{Names: []string{"svc1"}})
	if err != nil {
		panic(err)
	}

	// wait for the change
	_, err = pebble.WaitChange(changeID, &client.WaitChangeOptions{})
	if err != nil {
		panic(err)
	}
}
```

### Python client

Here is an example to achieve the same result with Python:

```python
import ops

client = ops.pebble.Client("/home/ubuntu/PEBBLE_HOME/.pebble.socket")

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
changeID = client.start_services(["svc1"])

# wait for the change
client.wait_change(changeID)

#  get services, the service svc1 should be active
services = client.get_services()
for svc in services:
    print(f"The status of service {svc.name} is: {svc.current}.")

# stop services
changeID = client.stop_services(["svc1"])

# wait for the change
client.wait_change(changeID)
```

## See more

- [Go client](https://pkg.go.dev/github.com/canonical/pebble/client)
- [Python client for Pebble API](https://ops.readthedocs.io/en/latest/reference/pebble.html)
- [API and clients](../explanation/api-and-clients)
- [API](../reference/api)
