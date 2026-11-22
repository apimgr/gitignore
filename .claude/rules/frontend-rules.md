# Frontend Rules (PART 16)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

## CRITICAL - NEVER DO

- Never serve untrusted active content as executable same-origin HTML/SVG/XML.
- Never regress accessibility, mobile behavior, or theme contrast.
- Never put runtime state or generated files in the source tree.
- Never require JavaScript for core page use or language selection; do not use inline scripts or event handlers.

## CRITICAL - ALWAYS DO

- Keep SSR pages, PWA behavior, offline assets, browser interaction, and public API behavior consistent with the project scope.
- Support light, dark, and automatic themes and accessible responsive layouts.
- Keep frontend text translated and page metadata, sitemap, and verification behavior aligned with the spec.
- Keep every required `/server/*` page current, accessible, translated, and sourced from IDEA.md where specified.
- Use server-rendered templates, safe POST/CSRF handling, and the specified public/credential-gated page behavior.

## KEY RULES

- Pages are server-rendered, usable without JavaScript, responsive, accessible, and translated.
- Every required server page reflects current product behavior and uses safe request handling.

For complete details, see AI.md PART 16.
