# API

Pebble exposes an HTTP API that remote clients can use to interact with the daemon. The API has endpoints for starting and stopping services, adding configuration layers to the plan, and so on.

The API uses HTTP over a Unix socket, with access to the API controlled by user ID. If `pebble run` is started with the `--http <address>` option, Pebble exposes a limited set of open-access endpoints (see {ref}`api-access-levels`) using the given TCP address.

## Structure of the API

Here is a breakdown of the general structure of most API endpoints in Pebble.

### API endpoint URL

Pebble API uses standard HTTP URLs to get actions and resources. The URLs are made up of a base URL followed by a path and optional query parameters.

- Base URL: The consistent foundation of all API endpoints (for example, `http://localhost:4000/v1/`).
- Path: Specifies the resource or collection being accessed (for example, `/changes`, `/services`). Paths are hierarchical and use forward slashes as separators.
- Query parameters: Optional key-value pairs appended to the URL after a question mark (?) and separated by ampersands (&). They provide additional filtering or control over the request (for example, `/services?name=svc1`).

Example:

```bash
http://localhost:4000/v1/services?name=svc1
```

### Request

Key components of a request include:

- HTTP Method: The desired action (for example, `GET` for retrieving data, `POST` for creating data, `PUT` for updating data, and `DELETE` for deleting data).
- Headers: Metadata about the request, such as content type, basic auth, and accepted response formats (for example, `Authorization: Basic <credentials>`, `Content-Type: application/json`).
- Body: For requests that send data to the server (`POST`, `PUT`), the body contains the data in JSON format.

### Response

After processing a request, Pebble sends back a response. Key components of a response are:

- HTTP status code: the result of the request.
- Headers: metadata about the response, such as content type and caching information.
- Body: data returned by Pebble server in JSON format.

### JSON format

Pebble API uses JSON for data exchange. Both request and response bodies are formatted as JSON objects or arrays.

Example JSON Response:

```json
{
  "type": "sync",
  "status-code": 200,
  "status": "OK",
  "result": {
    "name": "svc1",
    "startup": "disabled",
    "current": "stopped"
  }
}
```

### Request types

Pebble API supports both synchronous and asynchronous requests.

- Synchronous requests: The client sends a request and waits for the server to return a response before continuing.
- Asynchronous requests: For long-running operations, Pebble API offers asynchronous processing. The client sends a request and gets an acknowledgment immediately, but the actual processing is still running on the server side. The client can poll for the result. Specific endpoints supporting asynchronous operations are documented below with `type` as `async`.

## Using cURL

To access API endpoints over the Unix socket, use the `--unix-socket` option of `curl`. For example:

* TODO

    ```
    curl ...
    ```

* TODO

    ```
    curl ...
    ```

## Using the Go client

To use the Go client to access API endpoints over the Unix socket, first create a client using `New`, and then call the methods on the returned Client to interact with the API. Example:

```go
import (
  "log"

	"github.com/canonical/pebble/client"
)

func main() {
	pebble, err := client.New(&client.Config{Socket: ".pebble.socket"})
	if err != nil {
		log.Fatal(err)
	}
	changeID, err := pebble.Stop(&client.ServiceOptions{Names: []string{"mysvc"}})
	if err != nil {
		log.Fatal(err)
	}
	_, err = pebble.WaitChange(changeID, &client.WaitChangeOptions{})
	if err != nil {
		log.Fatal(err)
	}
}
```

We try to never change the underlying HTTP API in a backwards-incompatible way. However, in rare cases we may change the Go client in a backwards-incompatible way.

For more information, see the [Go client documentation](https://pkg.go.dev/github.com/canonical/pebble/client).

## Using the Python client

The Ops library for writing and testing Juju charms includes a [Python client for the Pebble API](https://ops.readthedocs.io/en/latest/reference/pebble.html). You can use the Python client to access API endpoints over the Unix socket. For example:

```python
import ops
client = ops.pebble.Client('/path/to/.pebble.socket')
client.start_services(['svc1', 'svc2'])
```

For more information, see:

- [Ops library documentation for the Python client](https://ops.readthedocs.io/en/latest/reference/pebble.html#ops.pebble.Client)
- [Source code of the Python client](https://github.com/canonical/operator/blob/main/ops/pebble.py)
- [Pebble in the context of Juju charms](https://juju.is/docs/sdk/interact-with-pebble)

For more examples, see "How to use the Pebble API". <!-- [David] Link to the how-to guide -->

## Data formats

Some API parameters use specific formats as defined below.

### Duration

The format of the timeout duration string is a sequence of decimal numbers, each with optional fraction and a unit suffix, such as "300ms", "-1.5h" or "2h45m".

Valid time units are "ns", "us" (or "µs"), "ms", "s", "m", "h".

### Time

A timestamp as a quoted string in the [RFC 3339](https://datatracker.ietf.org/doc/html/rfc3339) format with sub-second precision. Here are some examples:

- "2006-01-02T15:04:05Z07:00"
- "2006-01-02T15:04:05.999999999Z07:00"
- "2024-12-18T10:45:29.919907089+08:00"

## Errors

API endpoints may return errors as 4xx [status codes](https://www.iana.org/assignments/http-status-codes/http-status-codes.xhtml) (client errors) and 5xx status codes (server errors).

Errors are always returned in JSON format with the following structure:

```json
{
  "type": "error",
  "status-code": 500,
  "status": "Internal Server Error",
  "result": {
    "message": "error message",
    "kind": "error kind",
    "value": {},
  },
}
```

Possible values for `status-code`:

- 400: Bad request. For example, a badly-formatted parameter, like when starting a service, the service name is not provided.
- 401: Unauthorized.
- 404: Not found. For example, a change with the specified ID does not exist.
- 500: Internal server error, for example, the Pebble database is corrupt. If these errors keep happening, please [open an issue](https://github.com/canonical/pebble/issues/new).

Possible values for `result.kind`:

- login-required
- no-default-services
- not-found
- permission-denied
- generic-file-error
- system-restart
- daemon-restart

## API endpoints

<link rel="stylesheet" type="text/css" href="https://unpkg.com/swagger-ui-dist@5.11.0/swagger-ui.css" ></link>
<link rel="stylesheet" type="text/css" href="../../_static/swagger-override.css" ></link>
<div id="swagger-ui"></div>

<script src="https://unpkg.com/swagger-ui-dist@5.11.0/swagger-ui-bundle.js" charset="UTF-8" crossorigin> </script>
<script src="https://unpkg.com/swagger-ui-dist@5.11.0/swagger-ui-standalone-preset.js" charset="UTF-8 crossorigin"> </script>
<script>
window.onload = function() {
  // Begin Swagger UI call region
  const ui = SwaggerUIBundle({
    url: window.location.pathname +"../../openapi.yaml",
    dom_id: '#swagger-ui',
    deepLinking: true,
    presets: [
      SwaggerUIBundle.presets.apis,
      SwaggerUIStandalonePreset
    ],
    plugins: [],
    validatorUrl: "none",
    defaultModelsExpandDepth: -1,
    supportedSubmitMethods: []
  })
  // End Swagger UI call region

  window.ui = ui
}
</script>
