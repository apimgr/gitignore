// External Swagger UI initializer. Kept out of the page HTML so the strict
// Content-Security-Policy (script-src 'self', no 'unsafe-inline') is satisfied.
// The OpenAPI spec URL is read from the container's data-spec-url attribute.
window.addEventListener('load', function () {
  var el = document.getElementById('swagger-ui');
  if (!el || typeof SwaggerUIBundle === 'undefined') {
    return;
  }
  window.ui = SwaggerUIBundle({
    url: el.getAttribute('data-spec-url'),
    dom_id: '#swagger-ui'
  });
});
