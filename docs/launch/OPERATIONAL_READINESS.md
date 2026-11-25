# Operational readiness

What must be true, beyond working code, before Mise takes real orders. The live, evidence-backed view of these items is the admin
**Launch** page (`/launch`, permission `launch.read`), whose gates are defined in `services/api/internal/launch/launch.go`. This document
explains each area and its owner; `GO_LIVE_CHECKLIST.md` is the pass/fail list.

A configuration record is **not** an approval. Gates that depend on a business decision stay *Blocked* until the named owner records
a sign-off on the Launch page (stored in `launch_signoffs`, audited), even when the configuration exists.

## 1. Business configuration (owners outside engineering)

| Area | Launch gate(s) | Owner | What "ready" means |
|---|---|---|---|
| Payment provider | `payment_provider` | Finance | A provider chosen, merchant account approved, sandbox integration built and tested, then live keys enabled with written approval. Today only `testpay` exists and it is refused in production. **Blocked.** |
| Plans and prices | `subscription_pricing` | Business | Active plans with approved prices in the database. Demo plans do not count. |
| Menu | `published_menu`, `allergen_declarations`, `weekly_menu` | Culinary director | Approved recipes, published non-demo meals with allergens declared, a weekly menu open for ordering. |
| Photography | `meal_photography` | Marketing | Real, licensed photographs of real Mise meals, with licence records. |
| Delivery | `delivery_zones`, `delivery_windows`, `delivery_capacity`, `delivery_partners`, `proof_of_delivery_policy` | Operations | Zones, service days and windows, capacity, partner agreements and a proof-of-delivery policy configured **and** signed off. |
| Food safety | `cold_chain` | Quality | Written food-handling and cold-chain requirements; sign-off only. |
| Kitchen | `production_schedule` | Production manager | Production capacity and the weekly schedule configured and signed off. |
| Suppliers and stock | `supplier_approvals`, `inventory_readiness` | Procurement / inventory | Qualified suppliers; opening stock counted (non-critical). |
| Support | `support_contacts` | Support | Published support email, hours and response promise approved. |
| Legal | `legal_policies` | Legal | Privacy policy, terms, subscription, cancellation and refund terms reviewed by counsel. Current pages are drafts. **Blocked.** |
| Go-live | `business_signoff` | Owner | Final written approval to switch `SITE_MODE` to `launch`. |

## 2. Technical operations

| Area | Gate | Status after Sprint 9 | Evidence / next step |
|---|---|---|---|
| Email provider | `email_provider` | Blocked | SMTP driver works (Mailpit in tests). Needs SES (or another provider) verified out of sandbox, with DKIM/SPF/DMARC on the real domain, and one delivered message as evidence. |
| Domain and TLS | `domain_tls` | Blocked | No domain provided. TLS termination **not** rehearsed (the Caddy profile was not run locally; see `docs/testing/STAGING_REHEARSAL.md`). |
| Demo content hidden | `demo_content_hidden` | Evaluated live | `CATALOG_DEMO_ENABLED=false` in production; the public catalogue excludes demo meals. |
| Backups and restore | `backups_restore` | Blocked (production) | Local dump/restore rehearsal passed (`docs/testing/MIGRATION_AND_RESTORE_RESULTS.md`). Production needs an RDS point-in-time restore test (§3). |
| Monitoring | `monitoring` | Blocked | Alarms exist in Terraform (CloudWatch → SNS email). Needs a real `alarm_email`, a test alarm received, and named incident contacts (`INCIDENT_RESPONSE.md`). |
| Staging | `staging_verified` | Blocked (AWS) | Local production-compose rehearsal done; AWS staging not provisioned. |
| Security review | `security_review` | See `docs/security/SECURITY_AUDIT.md` | |
| Rollback | `rollback_plan` | Blocked | `ROLLBACK_RUNBOOK.md` written; no rollback rehearsed yet (needs staging and two releases). |

## 3. Backups and disaster recovery

| Item | Design | Verified? |
|---|---|---|
| Strategy | RDS automated backups with point-in-time recovery (`database.tf`: `backup_retention_period = var.db_backup_retention_days`, default **7 days**; window 02:00 Trinidad and Tobago time). Single server: nightly `pg_dump -Fc` copied off the server. | Design only |
| Encryption | RDS `storage_encrypted = true` (KMS); snapshots inherit it. Single server: encrypt dumps before upload. | Not verified (no AWS) |
| Retention | 7 days by default; the business should decide (financial records may need longer, e.g. a monthly snapshot kept for years). **Decision needed.** | — |
| Restore procedure | `ROLLBACK_RUNBOOK.md` §3; verification queries in `MIGRATION_AND_RESTORE_RESULTS.md` §2 | Rehearsed locally on a copy of the development database: identical row counts and checksums |
| Recovery objectives | Proposed: RPO ≤ 5 minutes (PITR), RTO ≤ 2 hours. **Proposals, to be approved by the business**; untested at production size. | No |
| Access | Database in private subnets, reachable only from the ECS tasks' security group; credentials in Secrets Manager. No operator access path (bastion/SSM) is defined yet: **decision needed** for incident access. | Design only |
| Secret recovery | Secrets live in Secrets Manager (versioned). Keep an offline, access-controlled copy of `DATA_ENCRYPTION_KEY`: without it, stored MFA secrets cannot be read. | No |
| Ownership | Engineering owns backups; a named person must check them monthly. **To be named.** | — |

**A production backup is not verified until a restore has been done**: before go-live, restore production's latest automated backup
to a scratch instance and run the verification queries.

## 4. Observability (what exists)

- Structured JSON logs with `request_id` on every request; no passwords, tokens or card data logged.
- `GET /api/v1/health` (liveness), `/api/v1/ready` (database), `/api/v1/version` (release and commit).
- Worker heartbeats (`worker_heartbeats`, migration 00014) shown on admin **System** with outbox backlog, event-consumer lag, failed
  jobs, failed emails and failed payment webhooks over 24 hours.
- CloudWatch metric filters and alarms (Terraform): `ApiErrors`, `JobsFailed`, `AdminLoginsFailed`.
- Not present: an external error-tracking service (e.g. Sentry) and uptime checks from outside AWS. Recommended before launch; needs
  a business decision on the provider.

## 5. Weekly operating rhythm (to be confirmed by operations)

The software supports this cycle; dates and owners are the business's to set: menu published → ordering until the cutoff
(`MENU_CUTOFF_WEEKDAY`/`TIME`, currently Thursday 23:59) → orders lock → production plan and purchasing → receiving → production and
quality checks → packing verification → dispatch → proof of delivery → reconciliation and reports.
