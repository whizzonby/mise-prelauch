# ADR-002: Database

Status: accepted (Sprint 1). Extends `prelaunch/003-postgresql.md`.

## Decision

- **PostgreSQL 17** is the system of record. **pgx v5** is the driver. **Goose** applies
  SQL migrations embedded in the binaries (`misectl migrate up`).
- **sqlc for new modules.** `identity` and `customers` are written as SQL in
  `queries.sql` and generated into typed Go (`internal/*/store`, committed). The schema
  sqlc reads is the Goose migrations directory, so there is one source of truth. CI runs
  `sqlc diff` and fails if generated code is stale. Pre-launch modules (`leads`,
  `referrals`, `events`, `admin`) keep their hand-written, parameterised SQL; they are not
  converted for its own sake.
- **Migrations are additive** (expand, then contract later). A migration never drops or
  rewrites data that a running release depends on. Columns and tables are removed in a
  later migration, once no release reads them, and only with approval. The full rules are
  in `docs/database/MIGRATION_STRATEGY.md`.
- **Keys and time.** UUID primary keys (`gen_random_uuid()`) for entities; identity
  `bigint` for append-only logs. Every instant is `timestamptz` and stored in UTC;
  business dates for Trinidad and Tobago (delivery days, menu weeks) will be `date`
  columns interpreted in `America/Port_of_Spain`, never derived by truncating a UTC instant.
- **Constraints in the database**: foreign keys with deliberate `ON DELETE`, CHECKs for
  enumerations, unique constraints for identity (canonical email), partial indexes.
- **Transactions** wrap every multi-row change (`db.InTx`), and audit entries and job
  enqueues join the same transaction.

## Consequences

- Two styles of data access coexist. That is acceptable: the boundary is the module.
- Developers need sqlc only when they change queries; it runs from Docker (`pnpm sqlc`).
