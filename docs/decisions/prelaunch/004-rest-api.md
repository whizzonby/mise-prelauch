# ADR-004: A versioned REST API

Status: accepted

## Decision

JSON over HTTP, resource-oriented, versioned in the path (`/api/v1`). One response
envelope (`data` or `error`, plus `meta.request_id`), stable machine-readable error codes,
and field-level validation errors. Documented in `docs/api/README.md`.

## Why

- Three kinds of client will use it: Next.js apps, a Flutter app, and operators with
  `curl`. REST is the lowest common denominator all of them handle well.
- The surface is small and CRUD-shaped. GraphQL or gRPC would add tooling without removing
  any real difficulty.

## Authentication

Bearer tokens, never cookies, on the API itself. That keeps it usable by a mobile app and
removes CSRF from the API's concerns. Browsers that need a cookie (the admin) keep it on
their own Next.js server, which holds the token and calls the API.

## Consequences

No generated contract yet. When the surface grows, write an OpenAPI description and
generate `packages/api-client` and the Flutter client from it.
