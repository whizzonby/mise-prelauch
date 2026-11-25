# Incident response

> **Blocker:** named people, phone numbers and an escalation rota have **not** been provided. The roles below must be filled with real
> people before launch (`GO_LIVE_CHECKLIST.md`, *Incident contacts*). No contacts are invented here.

## 1. Roles

| Role | Responsibility | Person |
|---|---|---|
| Incident lead | Decides, coordinates, communicates; one person at a time | **to be named** |
| Engineer on call | Investigates, rolls back, fixes | **to be named** |
| Operations lead | Kitchen, packing, deliveries; customer impact on the ground | **to be named** |
| Customer support | Customer messages and refunds within policy | **to be named** |
| Business owner | Approves customer communications, refunds beyond policy, payment-provider contact | **to be named** |

## 2. Severity

| Level | Examples | Response |
|---|---|---|
| SEV1 | Site or checkout down; payments taken but not recorded (or recorded twice); customer data exposed; food-safety issue | Start immediately, any hour |
| SEV2 | Worker stopped (no emails, renewals, deliveries syncing); one admin module broken; delivery run cannot be dispatched | Within 1 hour in operating hours |
| SEV3 | Cosmetic defects, a report wrong, a single customer affected with a workaround | Next working day |

A suspected **food-safety** issue (allergen mislabelled, cold chain broken, recall) is always SEV1 and follows the kitchen's
food-safety procedure first; software actions (quality hold, pausing a batch, stopping dispatch) support it, never replace it.

## 3. Where to look

| Signal | Where |
|---|---|
| Release and worker health | admin **System** (version, worker heartbeat, outbox backlog, consumer lag, failed jobs) |
| Readiness | `GET /api/v1/ready` |
| Errors | CloudWatch logs (JSON; filter by `request_id`) and alarms `ApiErrors`, `JobsFailed`, `AdminLoginsFailed`; single server: `docker compose logs api worker` |
| Payments | admin **Payments**; provider dashboard (once a provider is approved) |
| Audit | admin **Audit** (who did what, when) |

Every API response and log line carries a `request_id` (in the JSON `meta` and the logs). Error pages in the apps do not show it
yet, so match by time, account and route when a customer reports a problem.

## 4. Steps

1. **Declare**: open an incident note with severity, start time and incident lead.
2. **Stabilise**: roll back if a release caused it (`ROLLBACK_RUNBOOK.md`); disable a misbehaving automation rule (admin
   **Automation** → disable); put the site in waitlist mode by redeploying with `SITE_MODE=waitlist` if ordering must stop.
3. **Protect data**: sessions are stored server-side and can be revoked. Staff: `misectl admin signout -email …` (and `disable` for a
   compromised account). A customer can use *Sign out everywhere else* on the account security page; a password reset also revokes
   their sessions. For a wide compromise, revoke every open session with a reviewed SQL update
   (`UPDATE sessions SET revoked_at = now(), revoked_reason = 'incident' WHERE revoked_at IS NULL`), run by two people and recorded.
   Rotate any exposed secret (`BFF_SHARED_KEY`, SMTP credentials, payment-provider keys). `TOKEN_SIGNING_KEY` signs waitlist tokens
   and IP hashes, and derives `DATA_ENCRYPTION_KEY` when that is unset; do not rotate it without migrating encrypted MFA secrets.
   Keep logs; do not delete evidence.
4. **Communicate**: the business owner approves customer messages. Do not promise times you cannot meet.
5. **Resolve and verify**: `DEPLOYMENT_RUNBOOK.md` §7.

## 5. After the incident

Within five working days write: timeline, impact (customers, orders, money), root cause, what went well, actions with owners. Personal
data breaches may carry legal notification duties in Trinidad and Tobago: **legal advice is required**; this runbook does not
define them.
