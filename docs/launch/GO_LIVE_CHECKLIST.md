# Go-live checklist

Status as of the end of Sprint 9 (25 November 2026). The live, evidence-backed version is the admin **Launch** page (`/launch`): it
reads configuration from the database, records sign-offs (`launch_signoffs`, audited) and never reports "ready" while a critical gate
is open. This document mirrors it, plus the items only people can verify.

**Status key.** **Pass**: verified with evidence (linked). **Partial**: verified locally or in part; what is missing is stated.
**Blocker**: cannot go live until resolved; needs the named owner. **Fail**: tested and not working.

**Overall: not ready to launch.** Engineering is complete; the commercial launch is blocked by the business, provider and
infrastructure items below. No item is marked Fail.

## Infrastructure

| # | Item | Status | Owner | Evidence / what is needed |
|---|---|---|---|---|
| 1 | Approved domain | **Blocker** | Business | No domain chosen or provided. Gate `domain_tls`. |
| 2 | TLS | **Blocker** | Engineering | Configured (ALB/ACM in Terraform; Caddy for one server) but **not tested**: the local rehearsal ran without TLS. Needs the domain, then a certificate check on all three hosts. |
| 3 | Production secrets | **Blocker** | Engineering | Procedure in `DEPLOYMENT_RUNBOOK.md` §8. Secrets not created (no AWS account). The API refuses to start without them (rehearsed). |
| 4 | Production database | **Blocker** | Engineering | RDS defined in Terraform (`terraform validate` passes); not provisioned. Migrations verified fresh and upgraded to version 14 (`docs/testing/MIGRATION_AND_RESTORE_RESULTS.md`, `STAGING_REHEARSAL.md`). |
| 5 | Backups and tested restore | **Blocker** | Engineering | Local dump/restore rehearsal passed (identical counts and checksums). Production restore **not tested**; retention needs a business decision (`OPERATIONAL_READINESS.md` §3). Gate `backups_restore`. |
| 6 | Monitoring | **Blocker** | Engineering | Health, readiness, version, worker heartbeat and admin **System** page work (tested). CloudWatch alarms defined, not deployed; no alarm recipient; no external error tracker. Gate `monitoring`. |
| 7 | Staging deployment | **Partial** | Engineering | Production compose rehearsed locally with production settings (`STAGING_REHEARSAL.md`). AWS staging not provisioned. Gate `staging_verified`. |
| 8 | Rollback plan | **Blocker** | Engineering | `ROLLBACK_RUNBOOK.md` written; not rehearsed (needs staging and two releases). Gate `rollback_plan`. |
| 9 | Incident contacts | **Blocker** | Business | Roles defined in `INCIDENT_RESPONSE.md`; no people named. |

## Commerce

| # | Item | Status | Owner | Evidence / what is needed |
|---|---|---|---|---|
| 10 | Payment provider approval | **Blocker** | Finance | No provider chosen, no merchant account, no sandbox credentials. Only the `testpay` simulator exists, and production refuses it (rehearsed). Gate `payment_provider`. |
| 11 | Webhook verification | **Partial** | Engineering | Signature, replay window, idempotency, out-of-order and duplicate delivery tested against the simulator (Go tests; concurrency tool). Must be re-verified with the real provider's sandbox. |
| 12 | Approved pricing | **Blocker** | Business | 0 active non-demo plans with a price. Gate `subscription_pricing`. |
| 13 | Tax and delivery fee | **Blocker** | Business | Both zero and configurable (`BILLING_TAX_MODE=none`, `BILLING_DELIVERY_FEE_MINOR=0`) pending a decision. |

## Product and operations

| # | Item | Status | Owner | Evidence / what is needed |
|---|---|---|---|---|
| 14 | Published meal catalogue | **Blocker** | Culinary director | 0 published non-demo meals backed by a published recipe. Gates `published_menu`, `allergen_declarations`, `weekly_menu`. |
| 15 | Meal photography | **Blocker** | Marketing | Real, licensed photographs of Mise meals with licence records. Gate `meal_photography` (sign-off). |
| 16 | Delivery configuration | **Blocker** | Operations | Zones, windows, capacity and a proof policy exist only as demo/test data here. Production values and partner agreements needed. Gates `delivery_*`, `proof_of_delivery_policy`. |
| 17 | Food handling and cold chain | **Blocker** | Quality | Written requirements and sign-off. Gate `cold_chain`. |
| 18 | Production schedule and capacity | **Blocker** | Production manager | Gate `production_schedule`. |
| 19 | Suppliers and opening stock | **Blocker** | Procurement / inventory | Real approved suppliers; opening stock count (non-critical gate). |
| 20 | Email service | **Blocker** | Engineering / Business | SMTP path tested with Mailpit only. Needs SES (or another provider) out of sandbox on the real domain, with DKIM/SPF/DMARC and a delivered message as evidence. Gate `email_provider`. |
| 21 | Demo content hidden | **Pass (production config)** | Engineering | `CATALOG_DEMO_ENABLED=false` in production; the demo seeder refuses (rehearsed). The live gate checks it at launch. |

## Content and legal

| # | Item | Status | Owner | Evidence / what is needed |
|---|---|---|---|---|
| 22 | Legal approval | **Blocker** | Legal | Privacy, terms, subscription, cancellation and refund pages exist as drafts; counsel review needed. No binding text was invented. Gate `legal_policies`. |
| 23 | Support contacts | **Blocker** | Support | Published support email, hours and response promise. Gate `support_contacts`. |
| 24 | Business content | **Blocker** | Business | About and contact pages show only approved facts; business address, team and further claims await approval (Sprint 9 report §24). |

## Quality evidence

| # | Item | Status | Evidence |
|---|---|---|---|
| 25 | Security tests | **Partial** | `docs/security/SECURITY_AUDIT.md`: scans clean, RBAC/ownership/session/webhook tests pass. Open: F1 (AWS API host exposes customer/admin paths; High), F2–F3 (Medium). Gate `security_review`. |
| 26 | Performance tests | **Partial** | `docs/testing/PERFORMANCE_RESULTS.md`: local page timings, load and concurrency (two High defects found and fixed). Not on production hardware or real mobile networks. |
| 27 | Cross-browser tests | **Partial** | Chromium full suite; Firefox, WebKit and iPhone-sized WebKit on the customer specs (Sprint 9 report §21). No real Safari or real devices. |
| 28 | Accessibility checks | **Partial** | `docs/testing/ACCESSIBILITY_RESULTS.md`: axe 0 violations on customer pages, keyboard checkout, reduced motion, 7 widths. No screen-reader or manual audit. |
| 29 | End-to-end tests | **Pass** | `docs/testing/E2E_COVERAGE.md`: brief §38 and §39 mapped to passing tests. |

## Business sign-off

| # | Item | Status | Owner |
|---|---|---|---|
| 30 | Business go-live sign-off | **Blocker** | Owner. Recorded on the Launch page (`business_signoff`) only when every critical gate is verified. Then a release with `SITE_MODE=launch` switches the site from waitlist to ordering. |

Nothing in this checklist enables live payments or a public launch. Both need explicit written approval.
