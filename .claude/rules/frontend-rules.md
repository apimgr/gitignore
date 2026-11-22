# Frontend Rules (PART 16)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

## CRITICAL - NEVER DO

- Never serve untrusted active content as executable same-origin HTML/SVG/XML.
- Never regress accessibility, mobile behavior, or theme contrast.
- Never put runtime state or generated files in the source tree.
- Never require JavaScript for core page use or language selection; do not use inline scripts or event handlers.
- Never nest `.section-card` elements, render a card without its own labelled heading, or add page-specific wrappers around the shared shell.
- Never put a language selector in the header or bypass the shared `/server/*` chrome.

## CRITICAL - ALWAYS DO

- Keep SSR pages, PWA behavior, offline assets, browser interaction, and public API behavior consistent with the project scope.
- Support light, dark, and automatic themes and accessible responsive layouts.
- Keep frontend text translated and page metadata, sitemap, and verification behavior aligned with the spec.
- Keep every required `/server/*` page current, accessible, translated, and sourced from IDEA.md where specified.
- Use server-rendered templates, safe POST/CSRF handling, and the specified public/credential-gated page behavior.
- Use one `.container` with one width modifier, one `.page-title` h1, and one accessible `.section-card` per concern; help/preferences subpages are registry-backed.
- Keep every state-changing form as a POST with CSRF and make each flow work without JavaScript.
- Sanitize operator/config/registry/OpenAPI Markdown before rendering; use the strict policy for header/footer fragments.
- Resolve language from `?lang=`, cookie, quality-aware `Accept-Language`, then English; only explicit choices persist, and document `lang`/`dir` and footer language-link behavior.

## KEY RULES

- Pages are server-rendered, usable without JavaScript, responsive, accessible, and translated.
- Every required server page reflects current product behavior and uses safe request handling.
- CI page conformance covers each registered and error page, including header, skip links, labels, internal links, forms, tables, scripts, and registry coverage.

For complete details, see AI.md PART 16.
