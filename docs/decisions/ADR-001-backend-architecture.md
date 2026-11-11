# ADR-001: Backend architecture

Status: accepted (Sprint 1). Extends `prelaunch/001-go-backend.md` and `prelaunch/002-modular-monolith.md`.

## Context

The target stack names Go, Chi, a modular monolith and durable background jobs. The
pre-launch API is already a Go modular monolith: one module, three binaries (`api`,
`worker`, `misectl`), domain packages under `internal/`, shared `platform/` packages, and a
PostgreSQL job queue. Routing uses the Go standard library `http.ServeMux` with method and
path-parameter patterns (Go 1.22+), not Chi.

## Decision

1. **Keep the modular monolith**: one deployable, domains as packages. No microservices.
2. **Keep the standard-library router for now; do not migrate to Chi in Sprint 1.**
   - `ServeMux` already gives method matching, path parameters and route patterns in
     logs (`r.Pattern`). Every handler is a plain `http.Handler`, so a later switch to Chi
     is mechanical: Chi accepts the same handlers and middleware.
   - Moving 30 routes and the middleware chain would change no behaviour and would put
     every pre-launch test at risk for no gain this sprint.
   - **Revisit when** a module needs per-subtree middleware that is awkward to express
     as wrapping (for example, an `/api/v1/kitchen/*` group with its own scope check), or
     route groups become hard to read. That decision belongs to whoever builds that module.
3. **Domain boundaries.** Each domain package owns its tables, SQL, rules and handlers.
   Another domain calls it through exported functions or through an interface it
   defines, never by querying its tables. Sprint 1 examples:
   - `identity` defines `Hooks`; `customers` implements it. Identity does not import customers.
   - `customers` reads leads through `leads.FindForAccount` and `leads.RecordEvent`.
   - `admin` asks `identity.Service.RequireAdmin(permission, handler)`; it no longer owns auth.
   - `audit.Record` is the one way to write an audit entry.
   The full map is in `docs/architecture/MODULE_BOUNDARIES.md`.
4. **Routes are registered through `httpx.Router`** and recorded, so a test compares
   them with the OpenAPI contract.
5. **Background work** stays on the PostgreSQL `jobs` table (`prelaunch/008`). Enqueueing
   takes the caller's transaction, which makes it a transactional outbox: a job exists
   if and only if the change that asked for it committed. Retries with backoff, a
   permanent `failed` state (the dead-letter set, visible in the table and alarmed on in
   CloudWatch), stuck-job recovery and per-kind handlers already exist. Sprint 1 adds
   three job kinds (account emails) and nothing else. Payloads carry ids only.

## Consequences

- The stack differs from the brief in one named place (the router). This record is the
  place that says so; it is listed in the Sprint 1 report as a decision to confirm.
- Event versioning and idempotency keys for jobs are not needed by any Sprint 1 job:
  email jobs are idempotent by re-reading current state at send time. They are
  specified for later use in `DATA_FLOW.md`.
