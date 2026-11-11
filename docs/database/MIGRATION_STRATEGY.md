# Migration strategy

## Mechanics

- Goose SQL files in `services/api/migrations`, numbered `NNNNN_name.sql`, embedded into
  every binary. Each runs in a transaction.
- Applied by `misectl migrate up`: a one-off ECS task in AWS, the `migrate` service in
  Docker Compose, `mise.sh start` on a single server. It runs **before** new application
  code starts.
- `misectl migrate down` exists for local work only (refused unless `MISE_ENV=local`).
  Production rolls forward; it is never rolled back by running Down sections.
- `misectl doctor` checks that every permission the code checks exists in the database.

## Rules

1. **Additive first (expand, then contract).** A release may add tables, nullable
   columns, columns with defaults, and indexes. It may not drop, rename or retype anything
   the previous release reads. Removal happens in a later migration, after no deployed
   code uses the object, and **needs approval**.
2. **Never destructive sync.** No ORM auto-migration, no `DROP` in an Up section of a
   migration that runs against production data without approval.
3. **Data copies, not moves.** When data changes home (as admins did in 00004), copy it
   and keep the source until a later, approved cleanup.
4. **Large tables.** Create indexes `CONCURRENTLY` in their own migration
   (`-- +goose NO TRANSACTION`) once a table is large; backfill in batches from a job.
5. **Seed reference data in migrations** (roles, permissions), never in application startup.
6. **Every migration is tested**: CI applies all migrations to an empty database, rolls
   the latest back and re-applies it; data-moving migrations get an integration test that
   loads data into the previous version and migrates it forward
   (`TestMigrationCopiesPrelaunchAdmins`).

## Sprint 1: 00004_identity

| | |
|---|---|
| Adds | users, password_credentials, sessions, auth_tokens, mfa_totp, roles, permissions, role_permissions, user_roles, customers; `audit_log.actor_user_id`, `audit_log.request_id` |
| Copies | admin_users → users, password_credentials, user_roles (same ids and hashes) |
| Changes | nothing existing, apart from the two nullable audit columns |
| Fails safely if | two admins' emails canonicalise to the same address (unique violation; the whole migration rolls back) |
| Previous release on the new schema | works: it still reads admin_users and admin_sessions |
| Effect on people | admins sign in again (old sessions are not carried over) and enrol MFA where required |

**Running 00004 against production data needs approval** because it changes how existing
admins authenticate.

### Rollback

1. Redeploy the previous image tags (`deployment.md`, "Rollback"). The pre-Sprint-1 code
   runs against the 00004 schema unchanged, and its admins sign in with `admin_users` as before.
2. Leave the new tables in place. Anything created after the deploy (customer accounts,
   new staff, changed passwords) lives only in the new tables and would be lost by
   dropping them.
3. Only if the new tables must go: back up first, then `misectl migrate down` with
   `MISE_ENV=local` against a restored copy, never directly in production.

## Backups and restore

RDS automated backups (7 days staging, configurable in production) and point-in-time
recovery, defined in Terraform. **A restore has not been rehearsed**; do one before
launch: restore to a new instance, point a staging task at it, run `misectl migrate status`
and the smoke test. On the single-server path, take a `pg_dump` before every deploy that
includes a migration.
