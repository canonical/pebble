# API

Pebble exposes an API to allow remote clients to interact with the daemon. The API can start and stop services, add configuration layers to the plan, and so on.

## Accessing the API

The API uses HTTP over a Unix socket, with access to the API controlled by user ID. If `pebble run` is started with the `--http <address>` option, Pebble exposes a limited set of open-access API endpoints using the given TCP address. See [API access levels](). <!-- [David] Will we be able to use MD for internal links? -->

See below for some examples of how to use the API. For more examples, see []. <!-- [David] Link to the how-to guide -->

<!-- [David] I've adapted the next paragraphs from /explanation/api-and-clients/#controlling-api-access-using-identities -->

There's a Go client for the API. See the [Go client documentation](https://pkg.go.dev/github.com/canonical/pebble/client) and the examples below.

We try to never change the underlying HTTP API in a backwards-incompatible way. However, in rare cases we may change the Go client in a backwards-incompatible way.

There's also a Python client for the API. The Python client is part of the `ops` library used by Juju charms. See the [Ops documentation](https://ops.readthedocs.io/en/latest/).

### Go client examples

TODO: go client code snippets.

### cURL examples

TODO: curl examples.

## API access levels

API endpoints fall into one of three access levels, from least restricted to most restricted:

<!-- [David] I think we should move the content from /explanation/api-and-clients/#api-access-levels to here -->

### Identities

<!-- [David] I think we should move the content from /explanation/api-and-clients/#controlling-api-access-using-identities to here -->

## Common parameters

### timeout

The format of the timeout is a decimal number with an optional unit suffix (e.g., "300ms", "1.5s", "2h45m").

Valid time units are "ns", "us" (or "µs"), "ms", "s", "m", "h".

## Common errors

### 500

All APIs might return a 500 Internal Server Error response, but for the sake of simplicity, 500 responses are not documented in this API specification.

The HTTP 500 Internal Server Error response status code indicates that the Pebble server encountered an unexpected condition that prevented it from fulfilling the request. This error is a generic "catch-all" response to server issues, indicating that the server cannot find a more appropriate 5XX error to respond with.

---

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
