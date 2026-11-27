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

  var demoteNestedHeadings = function () {
    el.querySelectorAll('h1').forEach(function (heading) {
      var replacement = document.createElement('h2');
      Array.from(heading.attributes).forEach(function (attribute) {
        replacement.setAttribute(attribute.name, attribute.value);
      });
      while (heading.firstChild) {
        replacement.appendChild(heading.firstChild);
      }
      heading.replaceWith(replacement);
    });
  };
  var headingObserver = new MutationObserver(demoteNestedHeadings);
  headingObserver.observe(el, { childList: true, subtree: true });
  demoteNestedHeadings();
});
