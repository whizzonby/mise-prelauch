# ADR-001: Go for the backend

Status: accepted

## Context

The pre-launch platform needs an API today. The full Mise platform will need the same API
to serve a customer portal, internal ERP screens and a Flutter mobile app, so whatever is
chosen now is what the company builds on.

## Decision

The backend is written in Go, using the standard library HTTP server and router, `pgx`
for PostgreSQL and `log/slog` for logging. Dependencies are few on purpose: `pgx`,
`goose` (migrations), `x/crypto` (Argon2id) and `x/time` (rate limiting).

## Why

- One static binary per process: small images (the API image is about 56 MB), fast starts,
  cheap to run on Fargate.
- The language pushes toward explicit code, which suits a codebase that will be extended
  by people who did not write it.
- Since Go 1.22 the standard router handles methods and path parameters, so no web
  framework is needed.

## Consequences

- Types are not shared automatically with the TypeScript front ends. `packages/api-client`
  restates them by hand, and the end-to-end tests catch drift. If that becomes a burden,
  generate the client from an OpenAPI description.
- Form validation exists twice: Zod in the browser for feedback, Go on the server for
  truth. The server is the authority.
