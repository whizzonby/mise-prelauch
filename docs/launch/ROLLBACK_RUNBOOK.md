# Rollback runbook

Decide fast, roll back the application first, and treat the database separately. **Not yet rehearsed**: AWS was not available, and
the local production-stack rehearsal (`docs/testing/STAGING_REHEARSAL.md`) deployed one release only. Rehearse a real rollback on
staging before go-live (gate `rollback_plan`).

## 1. Decide

Roll back when a release causes any of: failed checkout or payment confirmation, errors on sign-in, wrong prices or totals, data
written incorrectly, the worker not processing (admin **System** heartbeat stale), or an alarm that started with the release.
If unsure, roll back; a forward fix can follow calmly.

Record in the incident channel: the time, the release being rolled back, the target commit (from `DEPLOYMENT_RUNBOOK.md` §2 step 5).

## 2. Application rollback

**AWS, automatic.** A release whose tasks fail health checks is rolled back by the ECS circuit breaker; the workflow fails. Confirm
with `curl https://<domain>/api/v1/version`.

**AWS, release passed health checks but is bad.** GitHub → Actions → **Deploy** → *Run workflow* from the **last good commit**
(production needs approval). Migrations already applied stay applied (see §3), which is why releases must be backward-compatible.

**Single server.**

```sh
cd /opt/mise
git checkout <last good commit>
RELEASE_VERSION=<previous version> RELEASE_COMMIT=$(git rev-parse --short HEAD) \
  docker compose -f docker-compose.prod.yml --env-file .env.production --profile caddy up -d --build
```

`migrate up` is a no-op when the database is already ahead of the code's migrations only if no new migration files were removed;
goose leaves newer versions alone. Verify with §4.

## 3. Database

- **Migrations are forward-only outside local development** (`misectl migrate down` is refused unless `MISE_ENV=local`). To undo a
  schema change, write and release a new migration.
- **Bad data written by a release**: fix forward with an audited admin action or a reviewed SQL script run by two people, recorded in
  the incident notes. Never edit append-only tables (audit log, inventory ledger, payment events); add compensating entries.
- **Data loss or corruption**: restore.
  - AWS: RDS point-in-time restore to a **new** instance, verify (row counts, latest orders and payments), then point the services'
    `DATABASE_URL` secret at it and redeploy. Payments taken after the restore point must be reconciled against the provider.
  - Single server: `pg_restore` the last good dump into a new database (procedure and verification queries:
    `docs/testing/MIGRATION_AND_RESTORE_RESULTS.md` §2), switch `DATABASE_URL`, restart.

## 4. Verify after rollback

Run `DEPLOYMENT_RUNBOOK.md` §7. Then: no checkout stuck in *pending* from the bad window (admin **Payments**), webhooks received during
the window processed (admin **System**: outbox and consumer lag), and customer-facing order states correct.

## 5. Afterwards

Write the incident record (`INCIDENT_RESPONSE.md` §5). The fix ships as a new release through the normal path.
