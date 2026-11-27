// External GraphiQL initializer. Kept out of the page HTML so the strict
// Content-Security-Policy (script-src 'self', no 'unsafe-inline') is satisfied.
// The GraphQL endpoint URL is read from the container's data-graphql-url attribute.
(function () {
  var el = document.getElementById('graphiql');
  if (!el || typeof GraphiQL === 'undefined') {
    return;
  }
  var fetcher = GraphiQL.createFetcher({ url: el.getAttribute('data-graphql-url') });
  var root = ReactDOM.createRoot(el);
  root.render(React.createElement(GraphiQL, { fetcher }));
  var controlLabel = el.getAttribute('data-control-label');
  var labelControls = function () {
    el.querySelectorAll('input, textarea, select').forEach(function (control) {
      if (control.labels && control.labels.length) return;
      if (control.getAttribute('aria-label') || control.getAttribute('aria-labelledby')) return;
      control.setAttribute('aria-label', control.getAttribute('placeholder') || control.getAttribute('title') || controlLabel);
    });
  };
  var observer = new MutationObserver(labelControls);
  observer.observe(el, { childList: true, subtree: true });
  labelControls();
})();
