# Sprint 4 plan: administration and CRM

Written 11 November 2026, on branch `sprint-4/admin-crm` from Sprint 3 (`4671c94`).

## Step 1: audit of Sprints 1 to 3

Checked against the code, not the reports.

| Area | Finding |
|---|---|
| Admin authentication | Opaque admin sessions with idle and absolute timeouts. TOTP MFA is required outside local (`ADMIN_MFA_REQUIRED`, refused off in production). `RequireAdmin(perm)` blocks staff who have not finished MFA. **Sound** |
| RBAC | Roles and permissions are in the database (`roles`, `permissions`, `role_permissions`, `user_roles`). A test ties code to migrations. `users.read` and `users.manage` exist but **guard nothing**: staff are managed only through `misectl` |
| Staff management | **Missing in the UI and API.** `misectl admin create/grant/revoke/disable/enable/reset-mfa/signout` work by email. There are no invitations |
| Customers | Profile, household, preferences, allergies, addresses and notification topics, all owned by `customers` and `addresses`. Staff can see **nothing** of customers today |
| Leads and referrals | Lead list, filters, detail, CSV export, erasure and a status `PATCH` exist. `leads.status` mixes verification (`PENDING`, `VERIFIED`), a staff mark (`QUALIFIED`), `CONVERTED`, `UNSUBSCRIBED` and `BLOCKED`. Referrals record `pending`/`converted`/`flagged`. There are no notes, assignment or CRM stage |
| Lead to customer | `customers.lead_id` is set after email verification; consent and preferences are copied once; the lead row is untouched. **Sound**, and it stays the only link |
| Commerce (Sprint 3) | Read-only admin for subscriptions, orders, payments, demand and promotions; plans and menus managed. Staff **cannot** pause or cancel on a customer's behalf |
| Audit log | `audit_log` records actor, action, target, metadata and request id, in the change's transaction. **Gaps:** no reason column, nothing stops `UPDATE`/`DELETE` (lead erasure deliberately deletes a lead's own entries), and there is no viewer |
| Notifications | Customer email only. There are no staff alerts |
| Jobs | The transactional job queue and the commerce scheduler in the worker. Failed jobs are visible (Sprint 3) |
| Reporting | `/admin/stats/overview` covers the waitlist only |
| CI | Workflows exist, but there is no remote, so they have never run |

**Unsafe or incomplete:** none blocks this sprint. Three are fixed here:
- the unused staff permissions get endpoints
- the audit log becomes append-only
- staff get a supported way to act on a subscription, instead of none

## Decisions

| Topic | Decision |
|---|---|
| One source of truth | CRM tables hold only CRM facts (stage, assignee, notes, cases, campaigns, segments). Customers, leads, subscriptions, orders and payments are read from their owning modules; nothing is copied |
| Lead stage and verification | A new `crm_lead_profiles.stage` (new, contacted, qualified, unqualified) sits beside `leads.status`, which is unchanged. "Converted" and "unsubscribed" are **derived** from the lifecycle (a linked customer; `UNSUBSCRIBED`), not set by staff. Leads already marked `QUALIFIED` start at stage `qualified` |
| Conversion | Still only through registration and email verification. A CRM stage never creates an account |
| Staff acting on subscriptions | Pause, resume and cancel on behalf of a customer go through the same `commerce` code as the customer's own request, with a required reason code, and are audited. There are no status overrides. Plan changes stay with the customer |
| Orders | Read-only: the later order statuses belong to the production and delivery modules. "Exceptions" are weeks in `needs_resolution` and payments that need attention |
| Sensitive data | Allergies, addresses and phone numbers need the new `customers.sensitive`. Everyone with `customers.read` sees identity, plan, orders and dietary preferences |
| Audit | Add `reason`. A trigger refuses `UPDATE` and any `DELETE`, except inside lead erasure, which sets a transaction-local flag. A viewer with `audit.read` |
| Reporting | One read-only `reporting` package aggregates across modules' tables. It never writes. Every metric has a written definition (below) |
| Exports | Asynchronous: a job writes the CSV into `report_exports`, downloadable for 24 hours by the requester only. Capped at 50,000 rows, CSV-injection safe, audited. The existing synchronous lead export stays |
| Staff alerts | `admin_alerts`, unique per dedupe key, each tagged with the permission needed to see it. Per-person read state. Raised by the worker's scheduler and by events (a new case) |
| Settings | Contact settings only (`business_settings`). Policies owned by configuration (cutoff, tax, fees, MFA, payments) are shown read-only, never duplicated |
| Integrations | Status worked out from configuration and real activity. Values: `not_configured`, `test_mode`, `configured`, `working` (with a last success). Nothing claims "connected" without evidence. No secrets are shown |
| Campaigns | Records, target segment, optional promotion and a UTM campaign key for attribution. **Nothing is sent**; a `MarketingProvider` boundary has no implementation |
| Frontend stack | Server-rendered pages with URL-driven filters, sorting and pagination, using the existing `@mise/ui` and console components. TanStack Table and Query, React Hook Form and shadcn are not added (ADR-006): the tables are server-paginated, so a client table library adds weight without function |

## Database (migration `00008_admin_crm.sql`, additive)

| Area | Tables |
|---|---|
| CRM | `crm_lead_profiles`, `crm_notes` (on a lead or a customer), `crm_segments` (explainable JSON rules) |
| Support | `support_cases`, `support_case_events` |
| Marketing | `marketing_campaigns` |
| Staff | `staff_invitations` |
| Alerts | `admin_alerts`, `admin_alert_reads` |
| Exports | `report_exports` |
| Settings | `business_settings` |
| Audit | `audit_log.reason` and the append-only trigger |

**New permissions:**
- `customers.read`, `customers.sensitive`, `customers.export`
- `leads.manage`, `cases.read`, `cases.manage`
- `campaigns.manage`
- `subscriptions.manage`
- `reports.read`, `audit.read`, `settings.manage`

`users.read` and `users.manage` stay the staff permissions.

## Metric definitions (all in Trinidad and Tobago time)

| Metric | Definition |
|---|---|
| Customers | Customer accounts with a verified email, whose user is not disabled |
| Active subscriptions | Status `active`, `pause_scheduled`, `past_due` or `cancel_scheduled` (still delivering or owing). `paused` is counted separately |
| New subscriptions | Subscriptions first activated (`activated_at`) in the period |
| Cancellations | Subscriptions that reached `cancelled` in the period (`cancelled_at`) |
| Orders this week | Orders for the current delivery week (Monday to Sunday) |
| Upcoming orders | Scheduled weeks whose cutoff is still ahead and that are paid: future boxes, not subscriptions |
| Invoiced (gross billed) | The sum of totals of invoices issued in the period, excluding void ones |
| Collected | The sum of succeeded payment amounts in the period (`succeeded_at`), refunds **not** deducted |
| Refunded | The sum of successful refunds in the period, shown on its own |
| Net collected | Collected minus refunded, labelled as such. **Not** profit |
| Outstanding | Billing cycles due or failed whose cutoff is still ahead |
| Failed payments | Payments that reached `failed` in the period |
| New leads | Leads created in the period. Conversions: leads linked to a customer in the period |

## API (admin, all permission-gated, in OpenAPI)

| Area | Routes |
|---|---|
| Dashboard | `GET /admin/dashboard` |
| Customers | `GET /admin/customers`, `GET /admin/customers/{id}` and `/orders`, `/billing`, `/preferences` (sensitive), `/activity`; `GET|POST /admin/customers/{id}/notes` |
| CRM | `GET|PUT /admin/leads/{id}/crm` (stage, assignee); `GET|POST /admin/leads/{id}/notes`; `GET|POST /admin/segments`, `PUT|DELETE /admin/segments/{id}`, `GET /admin/segments/{id}/preview`; `GET /admin/referrals`; `GET /admin/retention` |
| Support | `GET|POST /admin/support/cases`, `GET|PATCH /admin/support/cases/{id}`, `POST /admin/support/cases/{id}/notes` |
| Campaigns | `GET|POST /admin/campaigns`, `GET|PUT /admin/campaigns/{id}`, `POST /admin/campaigns/{id}/status` |
| Subscription actions | `POST /admin/subscriptions/{id}/pause|resume|cancel`, each with a reason code |
| Invoices | `GET /admin/invoices` |
| Reports | `GET /admin/reports/{kind}` (customers, subscriptions, orders, finance, marketing) |
| Exports | `POST /admin/exports`, `GET /admin/exports`, `GET /admin/exports/{id}/download` |
| Alerts | `GET /admin/alerts`, `POST /admin/alerts/read` |
| Staff | `GET /admin/staff`, `POST /admin/staff/invitations`, `GET /admin/staff/invitations`, `DELETE /admin/staff/invitations/{id}`, `POST /admin/staff/{id}/roles`, `DELETE /admin/staff/{id}/roles/{role}`, `POST /admin/staff/{id}/disable|enable|signout` |
| Invitation acceptance | `POST /admin/auth/invitations/accept` (public, with the token) |
| Roles | `GET /admin/roles` (a read-only matrix) |
| Audit | `GET /admin/audit-events` |
| Settings and integrations | `GET|PUT /admin/settings`, `GET /admin/integrations` |

## Admin app

- **Shell:** a sidebar grouped like the brief (Overview, Customers, CRM, Commerce,
  Catalog, Reports, Administration), showing only what the role allows. "Operations"
  modules stay hidden until they exist. An alerts link with an unread count.
- **Pages:** Overview; Customers (list and 360 with tabs); CRM (leads with stage, assignee
  and notes; segments; referrals; retention; campaigns); Support (cases); Commerce
  (existing, plus invoices and subscription actions); Reports; Exports; Administration
  (staff, roles, audit log, settings, integrations); Alerts.

## Security risks and how they are handled

| Risk | Handling |
|---|---|
| Privilege escalation through role grants | Only super_admin grants `super_admin` or `admin`. Nobody changes their own roles or disables themselves. The last active super_admin cannot be removed |
| Personal data in exports | A permission per export kind; minimal columns; requester-only download; 24-hour expiry; audited |
| Health data | Allergies are only behind `customers.sensitive`. They never appear in exports or audit entries |
| Audit tampering | The append-only trigger; no endpoint writes or deletes audit entries |
| Invitation tokens | Stored hashed; single use; 7-day expiry; can be revoked; the account gets MFA at first sign-in outside local |

## Tests

- **Go, against PostgreSQL:** every point in brief §36. Unauthorised and cross-role
  attempts on each new route. Report figures checked against known seeded data. The audit
  trigger. Invitation flow and role restrictions. Export permission and limits. Staff
  subscription actions sharing customer rules. Alert dedupe.
- **Playwright:** overview, customer search and 360, a lead note and stage, a support case
  lifecycle, invitation acceptance, the audit viewer, reports. Plus the existing 36.

## Progress

- [x] Step 1: audit
- [x] Backend: migration 00008, staff (identity), crm, support, reporting, backoffice, staff subscription actions. Sprint 4 Go tests pass (10 new); all earlier Go tests pass
- [x] OpenAPI and client; admin UI (shell, pages); e2e. Rechecked 11 November after a power loss: no damaged files; typecheck clean; Go 115 tests, Vitest 43, Playwright 43/43
- [x] Step 12: verification and report (12 November): one bug fixed (a support case's assignee name), one date-dependent test fixed; report in `docs/SPRINT_4_REPORT.md`
