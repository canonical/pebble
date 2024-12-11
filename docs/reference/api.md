# API

<link rel="stylesheet" type="text/css" href="https://unpkg.com/swagger-ui-dist@5.11.0/swagger-ui.css" ></link>
<link rel="stylesheet" type="text/css" href="../_static/swagger-override.css" ></link>
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
