# Sprint 4 report: administration and CRM

Written 12 November 2026. The work is on branch `sprint-4/admin-crm`, built on Sprint 3
(`4671c94`).
- The plan, the decisions and the metric definitions are in
  `docs/architecture/SPRINT_4_PLAN.md`.
- No new ADR was needed: the frontend stack stays as ADR-006 describes, and the backend
  stays the modular monolith of ADR-001.

Legend:
- **Verified:** built and covered by automated tests that pass.
- **Verified by hand:** built and checked on screen (the page renders correct data), with
  no automated test.
- **Implemented, not verified:** built but not exercised by a test or a person.
- **Blocked:** waiting on a business decision or external input.
- **Deferred:** left for a later sprint on purpose.

## 1. Existing-system audit findings

The full audit is step 1 of the plan. In summary:

| Area | Finding before Sprint 4 | Now |
|---|---|---|
| Admin sign-in | Opaque sessions, idle and absolute timeouts, TOTP MFA required outside local. Sound | Unchanged |
| RBAC | Roles and permissions in the database. `users.read` and `users.manage` guarded nothing; staff were managed only through `misectl` | Both now guard the staff API and screens |
| Customers | Staff could see nothing of customers | Customer list and 360 |
| Leads | List, detail, export, erasure, a status field mixing verification with staff marks. No notes, owner or stage | CRM stage, assignee and notes, kept apart from verification |
| Subscriptions | Read-only for staff; staff could not act for a customer | Pause, resume and cancel on a customer's behalf, with a reason |
| Audit log | No reason column, nothing prevented edits, no viewer | `reason`, an append-only trigger, a viewer |
| Reporting | Waitlist statistics only | Dashboard, five reports and background exports |
| Alerts | None for staff | Staff alerts with per-person read state |

Nothing found in the audit was unsafe enough to block the sprint.

## 2. Implemented admin modules

| Brief module | What exists | Status |
|---|---|---|
| Dashboard (§7, §8) | Figures with a "What this counts" definition each; a "needs attention" strip; weekly charts; live subscriptions by plan | Verified |
| Customer management (§9) | Search by name or email, filters, sorting, pagination | Verified |
| Customer 360 (§10) | Tabs: overview, orders, billing, preferences, activity, notes, support | Verified |
| CRM leads (§11) | Stage, assignee and notes on the lead page; the email status stays as it was | Verified |
| Lead conversion (§12) | Still only through registration and email verification; the 360 shows the lead and the referrer | Verified |
| Segments (§13) | Rules in plain terms, previewed against live data, explained on screen | Verified |
| Support (§14) | Cases with status, priority, assignee, history, notes and required resolution; linked to a customer or lead | Verified |
| Subscription administration (§15) | Pause and resume for the customer, through the customer's own rules, with a reason, audited and emailed | Verified |
| | Cancel for the customer, through the same code path | Implemented, not verified (the form was checked on screen; the shared cancel rules are tested in Sprint 3, the staff route is not) |
| Orders (§16) | Read-only list and detail (Sprint 3) | Verified |
| Payments and billing (§17) | Payments and refunds (Sprint 3); new invoice list with search and status filter | Verified (payments); invoice list verified by hand |
| Plans and menus (§18) | Sprint 3, unchanged | Verified |
| Campaigns (§19) | Records with target segment, optional promotion, UTM key and attribution. **Nothing is sent** | Verified |
| Retention (§20) | Five signals, each a fact rather than a prediction: failed renewal, recently cancelled, missed selections, inactive, stale case | Implemented, not verified |
| Referrals (§21) | List with flagged-only filter | Verified |
| Staff (§22) | Invite by email (7-day single-use link), grant and revoke roles, disable, enable, sign out everywhere | Verified, except "sign out everywhere": implemented, not verified |
| Roles (§23) | A read-only permission matrix | Verified |
| Audit (§24) | Viewer filtered by action, record, date; reason shown; entries cannot be edited or deleted | Verified |
| Settings (§25) | Business contact details editable; policies owned by configuration shown read-only | Verified |
| Integrations (§26) | Email, payments and analytics status worked out from configuration and real activity; no secrets shown | Verified |
| Reports (§27) | Customers, subscriptions, orders, finance, marketing, for a chosen date range | Verified |
| Exports (§28) | Background CSV, 50,000-row cap, CSV-injection safe, requester-only, 24-hour expiry, audited | Verified |
| Alerts (§29) | Raised by the scheduler (failed payment, a week needing resolution, failed job, menu unpublished near cutoff) and by new support cases; deduplicated; shown by permission | Verified |

The console's sidebar groups everything as the brief does and shows only what a role
may use. On a phone it becomes a scrolling top bar.

## 3. Database migrations

Migration `00008_admin_crm.sql` is additive and reversible.

| Area | Change |
|---|---|
| CRM | `crm_lead_profiles` (stage, assignee), `crm_notes` (on a lead or customer), `crm_segments` |
| Support | `support_cases` (numbered by a sequence), `support_case_events` |
| Marketing | `marketing_campaigns` |
| Staff | `staff_invitations` (token stored hashed) |
| Alerts | `admin_alerts` (unique per dedupe key), `admin_alert_reads` |
| Exports | `report_exports` |
| Settings | `business_settings` (contact keys only) |
| Audit | `audit_log.reason`, an action index, and the `audit_log_append_only` trigger |

- **Single source of truth:** CRM tables hold only CRM facts. Customers, leads,
  subscriptions, orders and payments are read from the tables that own them, with
  nothing copied.
- **Rollback:** down then up was run on the test database on 12 November. Down returns it
  to version 7 with the Sprint 4 tables gone; up returns it to 8.
- **Live data:** untouched. Tests use `mise_test` and the separate `mise-s1` stack.

## 4. API endpoints

There are 43 new paths, all under `/api/v1/admin`, all documented in
`docs/api/openapi.yaml`. The route contract test passes, and the TypeScript client is
regenerated.

| Area | Paths |
|---|---|
| Dashboard and reports | `dashboard`, `reports/{kind}` |
| Customers | `customers`, `customers/{id}` and its `orders`, `billing`, `preferences`, `activity`, `notes` |
| CRM | `leads/{id}/crm`, `leads/{id}/notes`, `segments`, `segments/preview`, `segments/{id}`, `referrals`, `retention` |
| Campaigns | `campaigns`, `campaigns/{id}`, `campaigns/{id}/status` |
| Support | `support/cases`, `support/cases/{id}`, `support/cases/{id}/notes` |
| Subscription actions | `subscriptions/{id}/pause`, `resume`, `cancel` |
| Billing | `invoices` |
| Exports | `exports`, `exports/{id}/download` |
| Alerts | `alerts`, `alerts/read` |
| Staff and roles | `staff`, `staff/invitations`, `staff/invitations/{id}`, `staff/{id}/roles`, `staff/{id}/roles/revoke`, `staff/{id}/disable`, `enable`, `signout`, `roles` |
| Invitation acceptance | `auth/invitations/accept` (public, needs the token) |
| Settings | `settings`, `integrations`, `audit-events` |

## 5. RBAC changes

There are 11 new permissions. `users.read` and `users.manage` now guard staff
management.

| Permission | super_admin | admin | support | operations | marketing | finance |
|---|---|---|---|---|---|---|
| `customers.read` | ✓ | ✓ | ✓ | ✓ | | ✓ |
| `customers.sensitive` (allergies, addresses, phones) | ✓ | ✓ | ✓ | ✓ | | |
| `customers.export` | ✓ | ✓ | | | | ✓ |
| `leads.manage` | ✓ | ✓ | ✓ | | ✓ | |
| `cases.read` | ✓ | ✓ | ✓ | ✓ | | ✓ |
| `cases.manage` | ✓ | ✓ | ✓ | | | |
| `campaigns.manage` | ✓ | ✓ | | | ✓ | |
| `subscriptions.manage` | ✓ | ✓ | ✓ | | | |
| `reports.read` | ✓ | ✓ | | ✓ | ✓ | ✓ |
| `audit.read` | ✓ | ✓ | | | | |
| `settings.manage` | ✓ | | | | | |

The table is a summary; the migration and the Roles page are the source.

**Rules enforced by the API:**
- Only a super_admin grants `super_admin` or `admin`.
- Nobody changes their own roles or disables themselves.
- The last active super_admin cannot be removed.

## 6. Screenshots

There are 18 pages at 1440 px and at 390 px, in `docs/screenshots/sprint-4/`:
- overview
- customers and customer 360
- leads and lead CRM
- segments
- campaigns
- support and a support case
- a subscription with staff actions
- invoices
- reports
- exports
- staff and roles
- audit log
- settings
- alerts

They are taken by `e2e/tools/admin-shots.mjs` against the running stack.

**Review fixes:** the review found one bug. A support case's own page always showed
"Assigned: Nobody", because the detail query did not join the assignee's name. The query
is fixed and a test now covers it.

## 7. CRM functionality

- **Lead stage:** `new`, `contacted`, `qualified` or `unqualified`, set by staff. It sits
  beside the lead's email status, which it never changes. "Converted" and "unsubscribed"
  are derived from the lifecycle, never set by hand. Leads already marked `QUALIFIED`
  started at stage `qualified`.
- **Owner and notes:** a lead or a customer can have an assignee. Notes are internal,
  attributed and time-stamped.
- **Conversion:** a CRM stage never creates an account. The link from lead to customer is
  still made only at email verification. Attribution and the referrer show on the 360.
- **Segments:** structured rules such as plan, status, region, tenure, source, never
  subscribed and lapsed. Each segment is explained in words and previewed with live
  members and a count.
- **Campaigns:** a record of name, channel, dates, target segment, optional promotion and
  a UTM key. Attribution counts customers and subscriptions arriving with that key. A
  `MarketingProvider` boundary exists with **no implementation**: nothing is sent.
- **Retention:** five factual signals with their definitions, each linking to the
  customers concerned. There is no churn prediction (brief §40).

## 8. Reporting functionality

- **One read-only module:** `reporting` aggregates across modules' tables and never
  writes.
- **Time zone:** every period is reckoned in Trinidad and Tobago time.
- **Definitions:** every figure carries its written definition on screen ("What this
  counts"). The definitions are those in the plan, for example:
  - collected is gross receipts before refunds, never profit
  - invoiced is billed, not necessarily paid
  - refunds are shown on their own
- **Reports:** five kinds (customers, subscriptions, orders, finance, marketing) for any
  date range, each with totals, a weekly series and breakdowns.
- **Accuracy:** a test seeds known subscriptions, payments and a refund, then checks the
  dashboard, the finance report's totals and its weekly series against them.
- **Exports:** customers, subscriptions, orders, payments and leads. Each needs its own
  permission and is built by a background job. Cells that start with `=`, `+`, `-` or `@`
  or a tab or carriage return are prefixed with `'`, so spreadsheets do not run them.

## 9. Security verification

| Risk | Handling | Evidence |
|---|---|---|
| Unauthorised access | Every new route needs a permission; signed-out requests are refused | 8 of the 10 new integration tests make refused wrong-role requests; the alerts test checks that each alert shows only to the right permission |
| MFA | Staff who have not finished MFA are refused wherever MFA is required | `TestStaffMustFinishMFAWhereRequired` |
| Privilege escalation | Rules in §5 | `TestStaffInvitationAndRoleRules` |
| Health data | Allergies and addresses only with `customers.sensitive`; never in exports or audit metadata; viewing them is audited | `TestCustomerListSearchAnd360`, e2e audit filter |
| Exports | Permission per kind, requester-only download, 24-hour expiry, row cap, CSV injection, audited | `TestExportsArePermissionedPrivateAndAudited` |
| Audit tampering | The trigger refuses `UPDATE` and `DELETE`; the only exception is lead erasure, which sets a transaction-local flag | `TestAuditLogIsAppendOnlyAndPermissioned` |
| Invitation tokens | Hashed, single use, 7 days, revocable | `TestStaffInvitationAndRoleRules`, e2e |
| Staff actions on subscriptions | The customer's own rules, a required reason, audited; no status overrides | `TestCustomerListSearchAnd360` |
| CSRF | Server actions, as in earlier sprints | Unchanged |

No new secret is shown anywhere: the integrations page states status only.

## 10. Tests executed (12 November 2026)

| Check | Result |
|---|---|
| Go gofmt and vet | clean |
| sqlc output | regenerated with sqlc 1.31.1; current |
| Go tests against real PostgreSQL | **115 pass**, 0 fail (104 before; 11 new) |
| OpenAPI route contract | pass (all 43 new paths documented) |
| ESLint and `tsc`, all workspaces | pass |
| Vitest | **43 pass** (web 15, admin 6, marketing 22) |
| Next.js production builds (marketing, admin, web) | pass |
| Playwright against the running stack | **43/43 pass**: the 36 earlier tests plus 7 new admin and CRM journeys. The full suite passed twice on 12 November, the second time after the last code change |
| Migration 00008 down and up | pass, on the test database |

The brief's §36 list, and where each point is tested:

| # | Point | Where tested |
|---|---|---|
| 1 | Admin authentication | `TestAdminAuth` (Sprint 1), e2e |
| 2 | MFA | `TestStaffMustFinishMFAWhereRequired` |
| 3, 18 | RBAC and cross-role attempts | 8 of the 10 new integration tests; e2e support-role journey |
| 4, 5 | Customer search and 360 visibility | `TestCustomerListSearchAnd360`, e2e |
| 6, 7 | Leads and attribution | `TestLeadStagesNotesAndConversion`, e2e |
| 8 | Support lifecycle | `TestSupportCaseLifecycle`, e2e |
| 9 | Subscription administration | `TestCustomerListSearchAnd360` (pause, resume) |
| 10, 11, 12 | Orders, payment read access, refund authorisation | Sprint 3 commerce tests |
| 13, 14 | Invitations and role restrictions | `TestStaffInvitationAndRoleRules`, e2e |
| 15 | Audit | `TestAuditLogIsAppendOnlyAndPermissioned`, e2e |
| 16 | Report accuracy | `TestReportFiguresMatchTheRecords` |
| 17 | Export authorisation | `TestExportsArePermissionedPrivateAndAudited`, e2e |
| 19, 20 | Earlier customer and commerce features | all earlier Go and Playwright tests, unchanged |

**Fixed during verification:** `TestReportFiguresMatchTheRecords` pinned the app clock to
11 November but let the database time-stamp payments, so it passed only before that
date. The test now puts both on the pinned clock. The product code was right.

**Not tested:**
- by any test: staff cancel, "sign out everywhere" and the retention list (built, not
  exercised)
- by an automated test: the invoice list (checked on screen)
- Safari, Firefox and real phones
- screen readers by hand
- load
- GitHub Actions (there is no remote)

## 11. Known issues

- **Wide tables on phones:** they scroll sideways inside their frame, using Sprint 3's
  shared table. The page itself never scrolls sideways, which the e2e tests check. A
  card layout for phones would read better.
- **MFA locally:** MFA is not required in the local environment, so local staff show
  "Two-step sign-in: not set up". Outside local, MFA is required and cannot be switched
  off in production.
- **The development PC's clock:** during the sprint the PC's clock lost about 39 hours
  while the machine was off, and the Windows Time service was stopped. Rows written then
  carry wrong times in the local test stack only. The clock was fixed on 12 November. If
  it drifts again, MFA codes and cutoff times will be wrong locally.
- **Charts:** weekly bars with a text summary and a definition, no interactive drill-down.
- **Alerts:** shown in the console only; no email or push for staff.

## 12. Outstanding business decisions

All decisions open from Sprint 3 still stand: the payment provider, tax, delivery fees,
refund policy and approved prices. Sprint 4 adds:

| # | Question | Current behaviour |
|---|---|---|
| 1 | Marketing email or WhatsApp provider | None; campaigns are records only |
| 2 | Who may see allergies and addresses | Support and operations, as well as admins (see §5) |
| 3 | How long exports and notes are kept | Exports expire after 24 hours; notes are kept |
| 4 | Business contact details | Empty until entered in Settings |
| 5 | Lead stages | `new`, `contacted`, `qualified`, `unqualified`; a sales process may want more |
| 6 | Retention thresholds | 30 days inactive, 3 days for a stale case, 2 missed weeks in 6 |

## 13. Sprint 5 readiness

**Ready:**
- Operations can see paid upcoming boxes, demand per meal (Sprint 3), and weeks that
  need resolution.
- The roles, audit log and alerts are in place for new modules to use.
- Reports and exports have one module to extend.

**Needed:**
- the business decisions above and those from Sprint 3
- a payment provider sandbox
- delivery partner details

**Deferred on purpose (brief §40):**
- Chef and Farmer ERP
- procurement
- production scheduling
- delivery dispatch
- demand forecasting and churn prediction
- mobile apps
- sending campaigns

Sprint 5 has not been started.
