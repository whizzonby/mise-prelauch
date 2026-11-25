# Migration and restore results (Sprint 9)

Run 14 November 2026 (PC clock; see the note at the end) on the developer PC, PostgreSQL 17 (`postgres:17-alpine`, compose project
`mise-s1`), with `misectl` built from the Sprint 9 working tree in `golang:1.27-alpine`. Helper: a script that runs
`go run ./cmd/misectl …` with `DATABASE_URL` pointing at a scratch database on the compose network.

These are **local rehearsals**. They prove the migrations and the backup/restore procedure work; they do **not** prove that production
backups exist or restore. That is a go-live gate (`docs/launch/GO_LIVE_CHECKLIST.md`).

## 1. Fresh database

| Step | Command | Result |
|---|---|---|
| Create | `CREATE DATABASE mise_s9_fresh` | ok |
| Up | `misectl migrate up` | `schema is at version 13` |
| Down ×13 | `misectl migrate down` (one step each) | `schema is at version 0`; 0 tables left besides `goose_db_version` |
| Up again | `misectl migrate up` | `schema is at version 13` |
| Doctor | `misectl doctor` | `ok: every permission the code checks exists in the database` |

## 2. Representative upgraded database (all sprints' data)

Source: the development database `mise` (35 MB on disk; 197 tables; 29,084 rows; data written by every sprint's workflows and e2e runs).

| Step | Result |
|---|---|
| `pg_dump -Fc -Z 6` | 2,598,077 bytes in 2 s; SHA-256 `8344861444b9e713ba9f9b1f1131f8d2d5247626372678997962eec45d94c8ef` |
| `pg_restore --no-owner --exit-on-error` into `mise_s9_restore` | ok in 6 s |
| Row counts, every table (197) | **identical** to the source |
| Checksums: Σ payments.amount_minor, Σ refunded_minor, Σ orders.total_minor, count(domain_events), md5 of the inventory ledger (id, on_hand_delta, on_hand_after), max(audit_log.id), md5 of order (id, status) | **identical** (`6435000 | 0 | 2937000 | 787 | 6cd20acc… | 8065 | ec5abee3…`) |
| Roll back migration 13 with data | **Failed** first time: `staff_invitations_role_key_fkey` (an open invitation to the `analyst` role). The migration runs in a transaction, so nothing changed (still version 13). |
| Fix | Down sections of 00009, 00010, 00011 and 00013 now remove pending invitations to the roles they remove, before the roles. Up sections unchanged. |
| Roll back all 13 migrations with data | every step ok, 13 → 0 |
| Roll forward | `schema is at version 13`; `misectl doctor` ok |
| Commerce event backfill on the restored data | `misectl events backfill-commerce`: **978 added**; second run **0 added** |

Backfilled events by type (restored copy): order.confirmed 89, order.status_changed 174, payment.failed 22, payment.succeeded 173,
subscription.activated 173, subscription.cancel_scheduled 24, subscription.cancellation_withdrawn 22, subscription.cancelled 80,
subscription.created 175, subscription.expired 2, subscription.pause_scheduled 22, subscription.resumed 22.

Scratch databases and the dump file were deleted afterwards. The development database was only read.

## 3. What this does not cover

- Production (RDS) automated backups and point-in-time restore: not run (no AWS access). Procedure in
  `docs/launch/OPERATIONAL_READINESS.md`, gate in the go-live checklist.
- Backup encryption at rest: an RDS setting in Terraform (`storage_encrypted`), not verifiable locally.
- Restore time at production size: the dataset here is small.

Clock note: this PC's clock has repeatedly fallen behind real time (see earlier sprint reports). Dates above are the PC clock.
