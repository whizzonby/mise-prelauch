# ADR-004: Monorepo

Status: accepted (Sprint 1). Extends `prelaunch/005-monorepo.md`.

## Decision

- Keep one repository: pnpm workspaces for TypeScript, one Go module for the backend.
  No task runner (Turborepo/Nx) until build times justify one.
- Add **`apps/web`**, the customer application (port 3102), beside `apps/marketing`
  (3100) and `apps/admin` (3101). Three apps, not route groups in one, because the three
  have different caching, security headers, cookie policies and audiences; the marketing
  site stays static-first and is never coupled to account code.
- Shared code lives in `packages/`: `ui` (design system, used by all three apps),
  `api-client` (types generated from `docs/api/openapi.yaml` plus a thin fetch client),
  `validation`, `analytics`, `config`.
- The brief's target tree is a reference. Deliberate differences:
  `services/api/cmd/worker` instead of `services/worker` (one Go module, shared domain
  packages); `e2e/` at the top level; no empty domain folders. Future domains are listed
  in `MODULE_BOUNDARIES.md` and created when their sprint starts.
- Generated code (sqlc, OpenAPI types) is committed and checked for staleness in CI.

## Consequences

`pnpm test` runs unit tests only; Playwright is `pnpm e2e` against a running stack.
`typecheck` runs `next typegen` first, so it works on a clean checkout.
