# Server Operations

GitIgnore is a public, read-only template service. Configure it with
`server.yml`, environment variables, or command-line flags. It has no user
accounts or admin web panel.

## Install and run

See [Installation](installation.md) for Docker, binary, and system service
options. Templates are embedded in the binary; no template volume or database
setup is needed to serve the catalog.

## Configuration

See [Configuration](configuration.md) for directory flags, environment
variables, server options, and update channels. A minimal configuration is:

```yaml
server:
  address: 0.0.0.0
  port: 8080
  baseurl: /
  api_version: v1
```

## Health and API documentation

- Health: `/healthz` or `/api/v1/server/healthz`
- OpenAPI: `/api/v1/server/swagger`
- GraphiQL: `/server/docs/graphql`
- GraphQL SDL: `GET /graphql`
- REST and GraphQL routes: [API Reference](API.md)

## Security

The API serves public catalog data without authentication. See the
[Security guide](security.md) for trusted proxy handling, debug endpoints, and
security reporting details.
