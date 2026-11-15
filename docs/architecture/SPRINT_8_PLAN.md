# Sprint 8 plan: analytics, automation and forecasting

Brief: `E:\FULL MISE DEV\SPRINT 8`. Audit: `SPRINT_8_AUDIT.md`. Branch `sprint-8/analytics-automation`.

## 1. Existing systems reused

Authoritative domain tables (audit §2); `platform/jobs`; `platform/consumer` (a third consumer,
`analytics`); `backoffice.Raise` for alerts; the export pipeline (`report_exports`, audited,
formula-safe CSV); `reporting` time-zone helpers (`menus.Location`, Trinidad and Tobago); the admin
shell, `MetricTile`, `DataTable`, `FilterBar`, `WeeklyBars`, `Definitions`; RBAC and audit.

## 2. Metric definitions (one registry)

`internal/analytics/metrics.go` holds every metric: id, name, definition, domain, source fields,
calculation, filters, time zone, period, update frequency, missing-data behaviour, currency,
limitations, owner, and its single SQL. Dashboards, snapshots, exports and scheduled reports all call
the registry, so no figure has two calculations. `docs/analytics/METRIC_CATALOG.md` is generated from
the registry (`misectl analytics catalog`), and a test fails if the file is out of date.

Money: TTD only. Other currencies are reported separately and never added (no conversion policy
exists). Ordered (order totals) is distinct from charged (payments attempted), collected (succeeded
payments), refunded (succeeded refunds) and net collected (collected − refunded). There is **no
recognised revenue**: no recognition policy exists, and the dashboard says so.

## 3. Data contracts

- **Date filters:** inclusive local dates in America/Port_of_Spain, converted to UTC bounds once.
- **Comparison:** the previous period of equal length.
- **Origin:** every money and demand metric is split into *verified* (non-test provider) and *test
  or demo*. While all payments are test payments, dashboards say so.

## 4. Storage strategy

There is no separate warehouse. Metrics are computed live with bounded ranges (at most 2 years) from
indexed authoritative tables. `analytics_snapshots` stores daily values per metric for history,
freshness and reconciliation (unique `(metric_id, day)`, upserted). `analytics_refresh_runs` records
each refresh. Data volume is small, so no materialized views (revisit with real volume).

## 5. Refresh and reconciliation

- **Refresh job** `analytics.refresh {from, to, reason}`: recompute snapshots for each day and upsert.
  It is idempotent, so a replay or backfill never duplicates anything.
- **Triggers:** the hourly worker tick (yesterday and today), the `analytics` outbox consumer (late
  events refresh the affected days), and a manual backfill (`misectl analytics backfill`, an admin action).
- **Reconciliation:** a data-quality check compares stored snapshots with live values for recent
  days and flags differences.
- **Freshness page:** last successful refresh, failures, consumer offset, and stale metrics.

## 6. Automation engine

- **Rule kinds (typed):** `procurement_shortage`, `overdue_purchase_order`,
  `production_hold_unresolved`, `delivery_failure_unresolved`, `run_unreconciled`, `payment_failed`.
  Each kind has a fixed subject query.
- **Parameters:** thresholds in hours, which ship **unset**. A rule can be enabled only with every
  required parameter set and an approval reference.
- **Actions:** `alert`, `task`, `request_approval`.
- **Triggers:** scheduled (each tick) and event (outbox consumer `automation`). Evaluation finds
  subjects, and each `(rule, subject)` occurrence is an execution with a dedupe key, inside the cooldown.
- **Execution:** a job with retries, failures visible, retry by hand. Executions record trigger,
  subject, action outcome and the audit trail.
- **Never actions:** refunds, charges, approvals of suppliers or orders, hold overrides, dispatch,
  plan changes. There is no such action type.

## 7. Rule safety model

- **Idempotency and duplicates:** `UNIQUE (rule_id, subject_key, window)`. Cooldowns are per rule
  and subject.
- **No cycles:** automation actions create alerts, tasks and approvals but write no outbox events,
  so automations cannot trigger each other.
- **Manual disable:** a disabled rule is not evaluated.
- **Approval:** `request_approval` creates a pending approval that only `automation.approve` can
  decide, and approving only records the decision and alerts the owning team.
- **Isolation:** a failing execution is marked failed in its own transaction, and core records are
  never touched.

## 8. Scheduled reports

`scheduled_reports`: kinds (daily operations, weekly business, weekly production and waste, weekly
procurement and inventory, monthly executive), period rule, and recipients (staff accounts holding
the report's permission). Recipients and enabling are business decisions, so reports ship disabled.
`report_runs` is unique per `(schedule, period_start)`, so each occurrence runs once. Content
(snapshot JSON plus CSV) is stored and downloadable. An email is queued per recipient only when
recipients exist and mail is configured. Each send's outcome (sent or failed) is recorded, and nothing
is claimed when only the job ran.

## 9. Forecasting architecture

- **Series:** confirmed meal-kit demand per meal per order week from `orders` (not pending or
  cancelled) and `order_items`, verified-only (orders paid through a non-test provider, non-demo
  meals). Test and demo orders are counted separately as contamination, never used.
- **Readiness gate:** weeks covered, observations, missing weeks, catalogue changes, outliers,
  cancelled versus confirmed, contamination, seasonal coverage. Activation thresholds ship **unset**:
  with no criteria, or criteria not met, the answer is "Not enough verified historical demand to
  generate a reliable forecast".
- **Methods:** `naive`, `seasonal_naive(s)`, `moving_average(k)`, `ses(alpha)`, pure functions
  behind a `Forecaster` interface.
- **Backtest:** rolling origin, time ordered, scoring MAE, WAPE (undefined when actual demand is
  zero), bias and MASE (when the in-sample naive error is not zero). A candidate is chosen only if it
  beats naive.
- **Uncertainty:** empirical residual quantiles from the backtest when there are at least the
  configured number of residuals. Otherwise "not estimated".
- **Versioning:** `forecast_runs`, `forecast_values` and `forecast_backtests` are append-only with
  a method version.
- **Ingredients:** projected through the production requirement engine (extracted, not
  duplicated), labelled as forecast, and never written to plans or purchase orders.

## 10. RBAC changes

New permissions (resource.action, one dot):
- Analytics: `analytics_executive.read`, `analytics_revenue.read`, `analytics_customers.read`,
  `analytics_production.read`, `analytics_procurement.read`, `analytics_inventory.read`,
  `analytics_delivery.read`, `analytics.export`.
- Automation: `automation.read`, `automation.manage`, `automation.execute`, `automation.approve`.
- Forecasting: `forecasting.read`, `forecasting.run`, `forecasting.configure`.

New role `analyst` (read every analytics area, export). Domain roles get their area (production
manager gets production analytics, etc.). Super admin and admin get everything.

## 11. UI/UX direction

- **Intelligence:** one dashboard renderer driven by the API's dashboard structure (sections of
  metrics with value, previous, change, definition, and tables), so every page looks and calculates
  the same way.
- **Pages:** Executive, Revenue & Subscriptions, Customers & Retention (cohorts), Meals,
  Production, Procurement, Inventory, Delivery, Data Quality & Freshness.
- **Automation:** rules, executions, tasks and approvals, scheduled reports.
- **Forecasting:** readiness, demand history, runs, model performance.

Existing tokens, no gradients, honest empty states.

## 12. Testing strategy

Go integration tests on real PostgreSQL for brief §45 items 1–29. Pure unit tests for forecasting
(synthetic series: stable, trend, seasonal, intermittent, zero, missing periods). Playwright for
executive dashboard, data quality, automation rule and execution, and forecasting readiness. Full
Sprint 1–7 regression.

## 13. Sprint 9 integration contracts

Metric registry and catalog (stable ids); dashboard JSON; automation execution and task records;
forecast run API; data-quality checks as a release gate input.

## Progress

- [x] Stage 1: audit (`SPRINT_8_AUDIT.md`), plan; Sprint 7 cost-record test added and passing
- [x] Stage 1b: metric registry (`internal/analytics/metrics.go`, 49 metrics), `docs/analytics/METRIC_CATALOG.md` generated by
  `UPDATE_CATALOG=1 go test ./internal/analytics` (a stale file fails the test).
- [x] Stage 2: migration 00013; snapshots + idempotent refresh/backfill (`analytics.refresh` job, `misectl analytics backfill`), outbox
  consumer `analytics` (refreshes the event's day), hourly tick in the worker, freshness status, data-quality checks (14) incl.
  snapshot reconciliation and ledger reconciliation.
- [x] Stage 3 (backend): dashboards (executive, revenue, subscriptions, cohorts, meals, production, procurement, inventory, delivery, cross-functional), audited formula-safe CSV exports. Test `TestAnalyticsReconcileWithAuthoritativeRecords` passes.
- [x] Stage 4: `internal/automation`: six typed rule kinds (shipped disabled; enabling refused without thresholds, cooldown and approval
  reference), versioned configuration, evaluation separate from execution, dedupe key per rule/subject/cooldown window, advisory lock,
  actions alert/task/request_approval only, retried idempotent jobs, failures visible with manual retry, approvals that never act,
  permission-scoped tasks, outbox consumer `automation` + scheduled tick. Test `TestAutomationRulesAreApprovedDedupedAndVisible`.
- [x] Stage 5: scheduled reports (5 kinds, disabled, no recipients): once per schedule+period (DB key), recipients must be active staff with
  the report permission, content from the analytics registry, CSV formula-safe, email outcome recorded (`skipped` without a real mail
  service: the log driver never counts as sent). Test `TestScheduledReportsRunOncePerOccurrence`. OpenAPI 1.8.0 (24 operations).
- [x] Stage 6: `internal/forecasting`: readiness gate (criteria ship unset; verified = confirmed, non-testpay, non-demo), naive /
  moving average / SES / seasonal naive, rolling-origin backtests (MAE, WAPE, bias, MASE; undefined values stay null), selection only when
  naive is beaten, empirical intervals only with enough residuals, append-only versioned runs, ingredient projection through
  `production.AggregateRequirements` (extracted from plan calculation, one BOM engine). Tests: model unit tests + `TestForecastingIsGatedAdvisoryAndUsesApprovedRecipes`.
- [x] Stage 7: admin UI: Intelligence (8 dashboards via one renderer, metric catalogue, data quality), Automation (rules, executions,
  tasks, scheduled reports), Forecasting (readiness and runs, run detail with backtests and ingredients, criteria). OpenAPI 1.8.0 (31 operations), client `intelligence` namespace.
- [x] Stage 8: verification (Go 16 packages ok, Vitest 43, Playwright 38), screenshots, `docs/SPRINT_8_REPORT.md`, commit
