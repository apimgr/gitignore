# GitIgnore API Server

GitIgnore serves a curated catalog of `.gitignore` templates through a versioned REST API, read-only GraphQL API, server-rendered browser interface, and companion CLI. The template catalog and interface locales are embedded in the static Go binaries.

## Quick start

The production Compose file is under `docker/`; copy it to a deployment directory so its persistent relative `volumes/` paths stay outside the source checkout. It listens on host address `172.17.0.1:64180` and routes to container port `80`.

From the deployment directory containing the copied Compose file, start it with:

```sh
docker compose -f docker-compose.yml up -d
```

Retrieve a template from a running instance:

```sh
curl http://172.17.0.1:64180/api/v1/templates/go.txt
```

## Features

- Search, browse, inspect, and combine templates in the browser.
- Retrieve and export the catalog over REST, or query it through GraphQL.
- Use unversioned gitignore.io-compatible routes.
- Run the companion client in a shell pipeline or download generated shell helpers and completions.
- Visit public About, Help, Privacy, Contact, Terms, Security, health, and API documentation pages.
- Use eight bundled locales: English, Arabic, German, Spanish, French, Italian, Japanese, and Chinese.

The service is read-only and public. It has no accounts, admin web panel, or user-submitted templates. The `/server/contact` route currently serves project contact information; it is not a message-submission endpoint.

## Documentation

- [Installation](installation.md)
- [Configuration](configuration.md)
- [API reference](api.md)
- [CLI reference](cli.md)
- [Security](security.md)
- [Integrations and discovery](integrations.md)
- [Development](development.md)
- [Translation guide](translating.md)

## Browser pages

The main interface includes `/`, `/search`, `/template/{name}`, `/combine`, `/categories`, `/list`, `/stats`, `/docs`, and `/cli`. Public server information pages are `/server`, `/server/about`, `/server/help`, `/server/privacy`, `/server/contact`, `/server/terms`, `/server/security`, `/server/security/policy`, and `/server/healthz`. Interactive API tools are at `/server/docs/swagger` and `/server/docs/graphql`.

## Links

- [Source repository](https://github.com/apimgr/gitignore)
- [Swagger UI](/server/docs/swagger)
- [GraphQL playground](/server/docs/graphql)

## License

The application source is MIT licensed; see [LICENSE.md](../LICENSE.md). Embedded template data can carry additional attributions and terms listed there.
