# Translating GitIgnore

GitIgnore embeds its interface locale files in the server and CLI binaries. `src/common/i18n/locales/en.json` is the source of message keys and structure; translators change values in other locale files and keep every key and placeholder intact.

## Supported locales

| Code | Language | Native name | Direction |
|---|---|---|---|
| `en` | English | English | Left to right |
| `ar` | Arabic | العربية | Right to left |
| `de` | German | Deutsch | Left to right |
| `es` | Spanish | Español | Left to right |
| `fr` | French | Français | Left to right |
| `it` | Italian | Italiano | Left to right |
| `ja` | Japanese | 日本語 | Left to right |
| `zh` | Chinese | 中文 | Left to right |

The locale registry is the source for supported language names, ordering, text direction, and plural categories. A locale is supported only when its complete file is embedded and passes the key and placeholder checks. Unsupported values fall back to English. The document `<html lang>` and `dir` attributes follow the resolved locale.

## Update a translation

1. Edit one locale JSON file other than `en.json`.
2. Preserve the English file's message keys, nesting, and key order; translate values only.
3. Preserve every interpolation placeholder exactly, including braces and spelling. `{count}` may be omitted in a plural category when the translated sentence does not need it; all other placeholders must remain in every category.
4. Include exactly the CLDR plural categories required by that locale. `other` is always required; an explicit `zero` form is also allowed.
5. Keep product names, protocols, code, filenames, configuration keys, URLs, and literal values such as `dark` and `auto` unchanged.
6. Use complete, natural sentences and consistent terminology. Follow the target language's typography and use a consistent register.
7. Check the page at 320, 768, and 1280 pixels. Verify Arabic and any other RTL locale in mirrored layout, and check that long translations do not clip or overflow.

## Glossary

Keep recurring product terms consistent across the UI. Preserve these terms when they refer to code or protocols:

| Term | Translation guidance |
|---|---|
| GitIgnore | Product name; do not translate |
| gitignore.io | External compatibility service name; do not translate |
| `.gitignore` | Filename; do not translate |
| API, REST, GraphQL, OpenAPI | Protocol or technology names; do not translate |
| CLI | Acronym; keep the acronym |
| template | Use one term consistently for an individual catalog entry |
| combine | Use one verb consistently for producing one file from multiple templates |
| category | Use consistently for a group of related templates |
| dark, light, auto | Configuration values; do not translate when shown as literal values |

Protocol names and literal values may appear within translated sentences; translate the surrounding sentence naturally.

## Add a locale

Copy `en.json` to `src/common/i18n/locales/{lang}.json`, where `{lang}` is a lowercase BCP 47 language code. Translate every value and fill the locale metadata: `language`, `name`, `native_name`, `direction`, and `version`. Add the locale to the single language registry and the runtime available-language defaults. Registry entries must include direction and the locale's CLDR plural categories. Add the language to this table and update the locale count in the root README in the same change.

The English source file is maintained by the project; translators do not change it as part of an ordinary translation contribution. When English strings change, keep every shipped locale in sync in that same change.

## Validation and review

The ordinary Go test suite validates locale key sets, empty values, interpolation variables, required plural categories, and orphaned keys. Run the project's `make test` workflow in its approved Go container. The locale JSON uses UTF-8, two-space indentation, no comments, and no trailing commas.

Reviewers check that the locale registry and embedded locale files match, placeholders and plural forms are valid, glossary terms are consistent, and the translated interface is usable at mobile, tablet, and desktop widths. Native-speaker review is welcome but is not a merge requirement. Translator credits are optional and are stored in the locale's `meta.translators` when requested.
