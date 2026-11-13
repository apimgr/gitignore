# Translating GitIgnore

GitIgnore ships seven interface locales in `src/common/i18n/locales/`. Locale files are embedded into the server and CLI binaries at build time. English (`en.json`) is the source locale; translation files use the same JSON keys and structure.

## Supported locales

| Code | Language | Native name | Direction |
|------|----------|-------------|-----------|
| `en` | English | English | Left to right |
| `ar` | Arabic | العربية | Right to left |
| `de` | German | Deutsch | Left to right |
| `es` | Spanish | Español | Left to right |
| `fr` | French | Français | Left to right |
| `ja` | Japanese | 日本語 | Left to right |
| `zh` | Chinese | 中文 | Left to right |

The locale registry defines the order used by the language selector. Unsupported locale requests fall back to English; a missing key in a translated file falls back to its English value.

## Update an existing translation

1. Choose one locale JSON file other than `en.json`.
2. Preserve every key and the nested structure from `en.json`; translate values only.
3. Keep placeholders such as `{count}` unchanged, including braces and spelling.
4. Keep product names, code, URLs, file names, configuration keys, and literal values such as `dark` and `auto` unchanged.
5. Use consistent terminology and a clear, neutral tone. Follow the target language's punctuation and typography conventions.
6. Open a pull request with the locale file change. Include the language and any translator credit you want recorded in the PR description.

## Add a locale

Adding a locale requires code and configuration changes because the server validates and orders supported languages. Copy `en.json` to a new lowercase language-code file, translate its values, preserve placeholders and keys, add the locale to the language registry and `server.i18n.available_languages`, and include the locale metadata (`language`, `name`, `native_name`, `direction`, and `version`). Then run the project test workflow and rebuild the server and CLI so the new embedded file is included. Update this table and the language count in the README in the same change.

## Review checklist

- The locale JSON parses and has the same key set as English.
- Placeholders and plural forms remain valid.
- The language registry and configured available-language list agree.
- Review the browser interface at 320, 768, and 1280 pixels; check right-to-left layout where applicable.
- The README's supported-language count matches the number of locale files.

## Translator credits

Locale metadata currently contains no named translator credits. Contributors may request a name or handle be added to the locale's `meta.translators` field; credits are optional.
