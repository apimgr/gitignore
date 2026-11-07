# Frontend Rules (PART 16)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

## CRITICAL - NEVER DO

- Never serve untrusted active content as executable same-origin HTML/SVG/XML.
- Never regress accessibility, mobile behavior, or theme contrast.
- Never put runtime state or generated files in the source tree.

## CRITICAL - ALWAYS DO

- Keep SSR pages, PWA behavior, offline assets, browser interaction, and public API behavior consistent with the project scope.
- Support light, dark, and automatic themes and accessible responsive layouts.
- Keep frontend text translated and page metadata, sitemap, and verification behavior aligned with the spec.

For complete details, see AI.md PART 16.
