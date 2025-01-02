# How to use Pebble API

Pebble, as a service manager, provides a command-line interface (CLI) for managing services, similar to systemd. However, Pebble distinguishes itself with its dedicated REST API.

While you can interact with systemd programmatically, it's typically done through [D-Bus](https://en.wikipedia.org/wiki/D-Bus) (a message-oriented middleware mechanism that allows communication between multiple processes running concurrently on the same machine), which has a steeper learning curve and requires more specialized code compared to using a REST API.

This is where Pebble shines, because in contrast, Pebble's REST API offers a simpler, more standardized approach to service management, making it easier to integrate into automated workflows like Continuous Integration/Continuous Deployment (CI/CD) pipelines, unlocking much more potential of Pebble.

---

## API and automated workflows

While the CLI is convenient for manual operations, the API enables integration with automated workflows.

Consider a CI pipeline that needs to setup some services before running tests and teardown the environment afterwards. Rather than relying on shell scripts and CLI commands which are error-prone and hard to maintain, the Pebble API offers a more robust and structured (service orchestration as code) approach. You can programmatically start, stop, and query the status and health of services before running tests in your CI workflows, allowing for tighter integration and more reliable automation.

For example, your CI workflow could start a service or even a group of inter-dependent services, verify services are running and health checks are OK, run integration tests against those services, and finally stop the services as a cleanup step. This automation reduces intervention, reduces risks of human errors, and ensures consistency and idempotency across different environments.

The Pebble API offers a significant advantage over CLI-based approaches in CI/CD pipelines. First of all, the structured interface simplifies interactions, eliminating the need for parsing command output. Second, API enables more sophisticated error handling. Last but not least, using the API promotes code reusability and maintainability, because instead of hard-coding shell commands throughout the CI workflow definition, we can encapsulate service operations within dedicated functions and modules, which leads to cleaner and more maintainable workflows.

---

## Pebble API

```{include} /reuse/api.md
   :start-after: Start: Pebble API overview
   :end-before: End: Pebble API overview
```

For more information, see [API](/reference/api).

---

## Using the API

You can use different tools and clients to access the API. For more examples, see [API and clients](../explanation/api-and-clients) and the [API reference doc](../reference/api).

### curl

At the moment (v1.17.0), only a handful of Pebble APIs are exposed via HTTP, most APIs are only available via the linux websocket. With curl, there is a `H       `--unix-socket <path>` parameter, which allows us to connect through this Unix domain socket instead of using the network. In this section, let's see how we can use curl to access Pebble API via the unix socket.

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

To add a simple layer with one service. run:

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

We can integrate these steps in our CI workflow to start services and dependencies, and now we can run some tests against them. Of course, after the CI workflow is finished, we want to do some cleanup like stopping those services.

To stop a service, run:

```bash
curl --unix-socket $PEBBLE/.pebble.socket -XPOST http://localhost/v1/services -d '{"action": "stop", "services": ["svc1"]}'
```

As aforementioned, this endpoint is `async`. We will get a response similar to:

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

---

## See more

- [Go client](https://pkg.go.dev/github.com/canonical/pebble/client)
- [Python client for Pebble API](https://ops.readthedocs.io/en/latest/reference/pebble.html). 
- [API and clients](../explanation/api-and-clients)
- [API](../reference/api)
