# Architecture decision records

| Record | Decision | Status |
|---|---|---|
| [ADR-001](ADR-001-backend-architecture.md) | Go modular monolith on the standard library router; Chi deferred | Accepted, Sprint 1 |
| [ADR-002](ADR-002-database.md) | PostgreSQL, pgx, Goose; sqlc for new modules; additive migrations | Accepted, Sprint 1 |
| [ADR-003](ADR-003-authentication.md) | One identity model, opaque server-side sessions, BFF cookies, TOTP for staff | Accepted, Sprint 1 |
| [ADR-004](ADR-004-monorepo.md) | One pnpm + Go monorepo; a third Next.js app for customers | Accepted, Sprint 1 |
| [ADR-005](ADR-005-infrastructure.md) | AWS as designed pre-launch; no new paid services in Sprint 1; tracing prepared, not deployed | Accepted, Sprint 1 |

`prelaunch/` holds the nine records written while building the pre-launch platform. They
are unchanged and still describe the code they cover; where a Sprint 1 record extends or
replaces one, the Sprint 1 record says so.
