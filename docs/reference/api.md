# API

Pebble exposes an HTTP API that remote clients can use to interact with the daemon. The API has endpoints for starting and stopping services, adding configuration layers to the plan, and so on.

The API uses HTTP over a Unix socket, with access to the API controlled by user ID. If `pebble run` is started with the `--http <address>` option, Pebble exposes a limited set of open-access endpoints (see {ref}`api-access-levels`) using the given TCP address.

## Using the API

You can use different tools and clients to access the API.

For more examples, see [How to use Pebble API](../how-to/use-the-pebble-api).

(api_curl)=
### curl

To access the API endpoints over the Unix socket, use the `--unix-socket` option of `curl`. For example:

```{terminal}
   :input: curl --unix-socket /path/to/.pebble.socket http://_/v1/services --data '{"action": "stop", "services": ["svc1"]}'
{"type":"async","status-code":202,"status":"Accepted","change":"42","result":null}
```

<br />

```{terminal}
   :input: curl --unix-socket /path/to/.pebble.socket http://_/v1/changes/42/wait
{"type":"sync","status-code":200,"status":"OK","result":{...}}
```

(api_go_client)=
### Go client

To use the [Go client](https://pkg.go.dev/github.com/canonical/pebble/client) to access API endpoints over the Unix socket, first create a client using `New`, and then call the methods on the returned `Client` struct to interact with the API. For example, to stop a service named `mysvc`:

```go
pebble, err := client.New(&client.Config{Socket: "/path/to/.pebble.socket"})
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
```

We try to never change the underlying HTTP API in a backwards-incompatible way. However, in rare cases we may change the Go client in a backwards-incompatible way.

For more information, see the [Go client documentation](https://pkg.go.dev/github.com/canonical/pebble/client).

(api_python_client)=
### Python client

The Ops library for writing and testing Juju charms includes a [Python client for Pebble API](https://ops.readthedocs.io/en/latest/reference/pebble.html). You can use the Python client to access the API endpoints over the Unix socket. For example:

```python
import ops
client = ops.pebble.Client("/path/to/.pebble.socket")
client.stop_services(["mysvc"])  # Python client also waits for change to finish
```

For more information, see:

- [Source code of the Python client](https://github.com/canonical/operator/blob/main/ops/pebble.py)
- [Pebble in the context of Juju charms](https://juju.is/docs/sdk/interact-with-pebble)

## Structure of the API

Pebble requests use the `GET` method for reads and the POST method for writes.

Some `GET` requests take optional query parameters for configuring or filtering the response, for example, `/v1/services?names=svc1` to only fetch the data for `svc1`.

All data sent to the API in `POST` bodies and all response data from the API is in JSON format. Requests should have a `Content-Type: application/json` header.

There are two main types of requests: synchronous ("sync"), and asynchronous ("async") for operations that can take some time to execute. Synchronous responses have the following structure:

```json
{
  "type": "sync",
  "status-code": 200,
  "status": "OK",
  "result": "<object-or-array>"
}
```

Asynchronous responses include a "change" field instead of "result", which is the ID of the [change](changes-and-tasks) operation that can be used to query the operation's status or wait for it to finish:

```json
{
  "type": "async",
  "status-code": 202,
  "status": "Accepted",
  "change": "<change-id>"
}
```

## Data formats

Some API parameters and response fields use specific formats as defined below.

### Duration

The format of a duration string is a sequence of decimal numbers, each with optional fraction and a unit suffix, such as "300ms", "-1.5h" or "2h45m".

Valid time units are "ns", "us" (or "µs"), "ms", "s", "m", "h".

### Time

A timestamp is a string in the [RFC 3339](https://datatracker.ietf.org/doc/html/rfc3339) format with sub-second precision. Here are some examples:

- "1985-04-12T23:20:50.52Z"
- "1996-12-19T16:39:57.123456789-08:00"

## Errors

API endpoints may return errors as 4xx [status codes](https://www.iana.org/assignments/http-status-codes/http-status-codes.xhtml) (client errors) and 5xx status codes (server errors).

Errors are always returned in JSON format with the following structure:

```json
{
  "type": "error",
  "status-code": 400,
  "status": "Bad Request",
  "result": {
    "message": "select should be one of: all,in-progress,ready"
  }
}
```

Possible values for `status-code`:

- 400: Bad request. For example, if a parameter is missing or badly-formatted, such as trying to start a service without providing a service name.
- 401: Unauthorized.
- 404: Not found. For example, if a change with the specified ID does not exist.
- 500: Internal server error. For example, if the Pebble database is corrupt. If these errors keep happening, please [open an issue](https://github.com/canonical/pebble/issues/new).

The `result` for some error types includes a `kind` field with the following possible values:

- daemon-restart
- generic-file-error
- login-required
- no-default-services
- not-found
- permission-denied
- system-restart

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

  function addSwaggerTagsToTOC(tags) {
    // Find the last H2 entry in the TOC and insert a 'ul' element for the tag list
    const tocContainer = document.querySelector(
      ".toc-tree > ul > li > ul > li:last-child"
    );
    const tocList = document.createElement("ul");
    tocContainer.appendChild(tocList);
    // Add a link for each tag inside the 'ul' element
    for (const tag of tags) {
      // Create an 'a' element for the tag link
      const tocLink = document.createElement("a");
      tocLink.classList.add("reference", "internal");
      const urlFriendlyTag = tag.replace(/ /g, "-");
      tocLink.href = `#/${urlFriendlyTag}`;
      tocLink.innerText = tag;
      tocLink.addEventListener("click", event => {
        if (event.shiftKey || event.ctrlKey || event.altKey || event.metaKey) {
          return;
        }
        // When the tag link is clicked with no modifier keys:
        // - Scroll the tag section into view
        // - If the tag section is closed, open it (by simulating a click)
        const operationsTag = tag.replace(/ /g, "_");
        const swaggerHeading = document.getElementById(`operations-tag-${operationsTag}`);
        swaggerHeading.scrollIntoView({
          behavior: "smooth"
        });
        if (swaggerHeading.getAttribute("data-is-open") == "false") {
          swaggerHeading.click();
        }
      });
      // Wrap the tag link in a 'li' element and add it to the tag list
      const tocItem = document.createElement("li");
      tocItem.appendChild(tocLink);
      tocList.appendChild(tocItem);
    }
  }

  // Make sure to match the tags defined in openapi.yaml
  addSwaggerTagsToTOC([
    "changes and tasks",
    "checks",
    "exec",
    "files",
    "health",
    "identities",
    "layers",
    "logs",
    "notices",
    "plan",
    "services",
    "signals",
    "system info"
  ]);
}
</script>
