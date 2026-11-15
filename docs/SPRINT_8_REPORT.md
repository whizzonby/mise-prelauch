# Sprint 8 report: analytics, automation and forecasting

Branch `sprint-8/analytics-automation`, cut from the Sprint 7 review fix (`8edeac0`). Sprint 8 is complete; Sprint 9 has not been started and
needs explicit authorisation.

Status words used throughout:

- **Verified**: exercised by an automated test that passed in this sprint's final run (section 23).
- **Implemented, not verified**: built and reviewed, but no test exercises it end to end.
- **Blocked**: built, but cannot operate until a business decision is recorded (section 25).
- **Deferred**: deliberately not built.

The single most important fact about this sprint's numbers: **every payment in every database so far was taken by the simulated test
provider (`testpay`)**. The dashboards say so on screen, the data-quality checks flag it, and forecasting refuses to run on it.
Nothing in this report describes real business performance.

---

## 1. Repository audit findings

Full audit: `docs/architecture/SPRINT_8_AUDIT.md`. The points that shaped the design:

- **Commerce emits no outbox events.** Payments, subscriptions and orders change without `domain_events` rows, so payment and subscription
  analytics and the `payment_failed` automation are driven by schedule, not events. Production, procurement, inventory and delivery do emit
  events, and those refresh analytics and trigger rules.
- **`payments` has no `subscription_id`.** Revenue by plan joins `payments → billing_cycles → subscriptions`.
- **Procurement shortages are not a column.** They are read from `procurement.shortage_identified` event payloads.
- **Existing reports** (`/reports/*`, Sprints 2–7) each calculated their own figures. They were left working; the new metric registry is the
  single definition for everything built in Sprint 8. Moving the old reports onto the registry is a Sprint 9 candidate (section 26).
- **Sprint 7 review corrections** (delivery week rule, delivery/commerce reconciliation) were re-checked and hold. One gap was found and
  fixed: delivery cost records had no test. `TestDeliveryCostRecords` now covers validation, permissions, kinds, report sums and audit.

## 2. Architecture decisions

Full plan: `docs/architecture/SPRINT_8_PLAN.md`.

1. **One metric registry** (`internal/analytics/metrics.go`). Each metric has an ID, name, definition, domain, sources, calculation, filters,
   period, update frequency, missing-data rule, currency rule, limitations, owner, unit kind, permission and one SQL expression. Dashboards,
   CSV exports, snapshots, scheduled reports and the catalogue document all read the registry. No figure is calculated anywhere else.
2. **Live first, snapshots second.** Dashboards calculate live from authoritative tables in a read-only transaction with a statement timeout.
   Additive metrics are also stored as daily snapshots (`analytics_snapshots`, upserted by `(metric_id, day)`) for series and reconciliation.
   No separate warehouse.
3. **Local days.** All ranges are Trinidad and Tobago local dates converted once to UTC bounds.
4. **Test and demo data are labelled, not hidden.** A dashboard carries notices with the share of money through `testpay` and the count
   of demo meals ordered. Forecasting excludes both.
5. **Automation is typed recipes, not code.** Six rule kinds with validated parameters. Actions are limited to alert, task or request
   approval. Evaluation is separate from execution, and execution runs as a retried, idempotent job.
6. **Forecasting is gated and advisory.** Baseline statistical methods only, each tested by rolling-origin backtest against the naive
   forecast. Activation criteria ship unset, so forecasting is inactive until the business decides them.
7. **One bill-of-materials engine.** Requirement aggregation was extracted from production plan calculation into
   `production.AggregateRequirements`. Plans and forecast ingredient projections both call it.

## 3. Metric catalog

52 metrics. Generated document: `docs/analytics/METRIC_CATALOG.md`, produced from the registry by
`UPDATE_CATALOG=1 go test ./internal/analytics`. A stale document fails `TestCatalogIsUpToDate`. `TestRegistryIsComplete` fails if a
metric lacks a catalogue field, uses an undeclared filter or has an unbound placeholder, or if a dashboard names an unknown metric.
In the console, every metric card has a "Definition" disclosure linking to `/intelligence/metrics`. **Verified.**

| Domain | Metrics |
|---|---|
| Revenue | ordered value, charged value, collected, collected via test provider, refunded, net collected, failed payments, failed cycles, outstanding |
| Orders | count, average value |
| Subscriptions | active, new, cancelled, expired, pauses, renewals, plan changes |
| Customers | new, first paid, repeat rate |
| Meals | kits ordered |
| Production | planned, produced, accepted, rejected, reworked kits; completion rate; batch hours; quality holds; waste records |
| Procurement | PO value, PO count, cycle days, on-time rate, rejected packs, overdue orders |
| Inventory | value on hand, lots without cost, quarantined lots, expired lots, issue movements, waste movements |
| Delivery | scheduled, delivered, failed attempts, first-attempt rate, on-time rate, returned, disputed, actual cost |
| Operations | open exceptions |

## 4. Data ingestion and aggregation

- **Refresh job** `analytics.refresh`: recalculates snapshot metrics for a range of days, one transaction per run, with the run recorded
  in `analytics_refresh_runs` (status written outside the work transaction so failures are kept). **Verified** (idempotent: a second refresh
  leaves row count and sums unchanged).
- **Backfill**: `misectl analytics backfill -from YYYY-MM-DD -to YYYY-MM-DD` or the "Refresh or backfill" form. *Implemented, not verified*
  (the CLI path; the job it enqueues is verified).
- **Event consumer** `analytics` (shared `platform/consumer` dispatcher): each production, procurement, inventory or delivery event refreshes
  the local day it happened. **Verified**: replaying every event from offset 0 changes no snapshot.
- **Worker tick**: dispatches events, then at most hourly (under an advisory lock) refreshes recent days.
- **Freshness**: snapshots are stale after 3 hours without a successful refresh. Stale status is shown on the data-quality page.
- **Reconciliation**: summed daily snapshots equal the live figure for the range. **Verified** in the test, and run as the
  `snapshot_reconciliation` quality check.

## 5. Revenue analytics

Cash view only. **There is no revenue recognition policy, so nothing is labelled recognised revenue.** Collected uses succeeded payments
(`succeeded_at` in range). Refunds are counted once from refund records, and the same refund form submitted twice is one refund. Failed
payments are never counted as collected. Test-provider money is reported separately as `revenue.collected_test`. **Verified**
(`TestAnalyticsReconcileWithAuthoritativeRecords`: collected, refunded, net, failed and the test-provider share all equal direct SQL on the
authoritative tables). Filter by plan.

## 6. Subscription analytics

Active (all live statuses, including paused and past due), new, cancelled, expired, pauses, renewals and plan changes, plus active
subscriptions by plan. **Verified**: active and new counts equal the subscription table.

## 7. Customer and cohort analytics

New customers, first paid, repeat rate, and cohorts by first-payment month. **A cohort's retention rates are shown only once it has at least
five customers**, so a cohort of two never shows "50% retention". Engagement signals are counts only. **Verified**: the cohort table counts
customers by first payment month.

## 8. Meal performance analytics

Kits ordered (confirmed orders only, never pending or cancelled), demand by meal, and weeks on menu (from menu availability). Filters: plan
and meal. **Verified**: kits ordered equals the fixture's confirmed kits.

## 9. Production analytics

Planned, produced, accepted, rejected and reworked kits; completion rate; batch hours; quality holds; waste records. **Produced = accepted +
rejected, and reworked kits are inside accepted, never added twice.** **Verified** (produced 6, accepted 5, reworked 2 in the test).

## 10. Procurement analytics

PO value and count, cycle days, supplier on-time rate (only POs with an expected date), rejected packs, overdue orders, and a supplier
table. Filter by supplier. *Implemented, not verified* by a dedicated assertion. The dashboard renders in the test suite and in the browser
test, but no test checks a procurement figure against SQL.

## 11. Inventory analytics

Value on hand (lots with a recorded cost; lots without cost are counted separately rather than valued at zero), quarantined and expired
lots, issue and waste movements, and a ledger check comparing lot balances to the movement ledger. **Verified** through the
`inventory_ledger_mismatch` quality check, which passed on consistent data.

## 12. Delivery analytics

Scheduled, delivered, failed attempts, first-attempt rate, returned, disputed and actual cost. **On-time rate is calculated only for
deliveries with a promised window**; otherwise it is "Not available", never 100%. Corrected and disputed outcomes come from the
commerce-owned `order_delivery_outcomes` (the Sprint 7 reconciliation contract). Filter by zone. *Implemented, not verified* by a dedicated
figure-level assertion. The Sprint 7 delivery report tests still pass.

## 13. Cross-functional analytics

"By order week" table on the executive overview: kits ordered, kits accepted, shortages identified, delivered, failed or returned, and
delivery cases for each order week. These are side-by-side facts; the page says a relationship between columns is not a cause.
**Verified**: the fixture week shows its 6 kits.

## 14. Automation engine

`internal/automation`. Six rule kinds (procurement shortage, overdue purchase order, unresolved quality hold, failed delivery without a next
step, unreconciled dispatch run, failed payment). **All ship disabled with thresholds unset.** Enabling is refused (422) until the
required parameters, a cooldown and an approval reference are recorded. **Verified**.

- **Versioning**: each save writes `automation_rule_versions`. **Verified**.
- **Dedupe and cooldown**: one execution per rule, subject and cooldown window. The window is enforced by a unique `dedupe_key` and an
  advisory lock, so a second worker or a repeated tick is a no-op. After the cooldown the same subject may trigger once more. **Verified**.
- **Only records after enabling**: `payment_failed` considers failures after `enabled_at`, so switching a rule on never floods the team
  with history.
- **Actions**: alert (admin alert, keyed by execution), task (one per execution, visible only to holders of the rule's owner permission),
  or request approval. **Automation never refunds, charges, approves suppliers or orders, overrides a quality hold, dispatches, or changes a
  subscription.** An approval records the decision and alerts the owning team; the test confirms payments and refunds are unchanged.
  **Verified**.
- **Failures**: a failing action marks the execution failed with the error, stays visible in the overview, and can be retried. A
  duplicate job delivery still creates one task. **Verified**.
- **Loop safety**: actions write no domain events, so one rule cannot trigger another.
- **Triggers**: scheduled evaluation in the worker tick, and the `automation` outbox consumer for event-driven kinds. The consumer path is
  *implemented, not verified* (the shared dispatcher is verified in earlier sprints).

## 15. Scheduled reporting

Five schedules (daily operations, weekly business, weekly production and waste, weekly procurement and inventory, monthly executive),
**all disabled with no recipients**. Content comes from the analytics dashboards; reports calculate nothing themselves.

- Once per occurrence: `UNIQUE (schedule_id, period_start)`. Two ticks generate one run, and "Generate now" for the same period is refused.
  **Verified**.
- Recipients must be active staff accounts holding the report's permission. Customers and kitchen staff are refused. **Verified**.
- Email outcomes are the mail service's own result (`sent` / `failed`). **The development `log` mail driver is treated as no email service**,
  so outcomes are recorded as `skipped` and never as sent. **Verified** for both cases.
- Stored content and CSV (formula-safe, audited download). **Verified**.

## 16. Forecasting methodology

`internal/forecasting`.

- **Series**: verified weekly demand per meal. Verified means confirmed orders (not pending or cancelled), paid through a non-test provider,
  for non-demo meals. Free weeks (no payment) are counted as unpaid and excluded. Each meal's series covers only weeks it was on a published
  menu, from its first verified order. The current, unfinished week is never history.
- **Readiness gate**: weeks of verified history, share of missing weeks, required improvement over naive, optional seasonal coverage, and
  an approval reference. Also reported: test-paid, unpaid and demo kits; cancelled vs confirmed orders; outlier weeks (beyond 3 IQR); meals
  new in the last 8 weeks. **With any criterion unset the answer is "Not enough verified historical demand to generate a reliable
  forecast."**
- **Methods**: naive, moving average (4 and 8), simple exponential smoothing (α 0.2 and 0.5), and seasonal naive when a season length is
  configured. These are pure functions behind a `Method` interface, each with a version.
- **Selection**: lowest pooled MAE, but only if it beats naive by the approved margin; otherwise naive is kept.
- **Uncertainty**: 10th–90th percentile of backtest residuals, shown only when there are at least the configured number of residuals.
  Otherwise "Not estimated".
- **Versioning**: `forecast_runs`, `forecast_values` and `forecast_backtests` are append-only (database triggers). A refused run is stored
  with the readiness that refused it.
- **Ingredients**: a run's expected kits for a week go through `production.AggregateRequirements` with each meal's published recipe for that
  week. The projection is computed on request, labelled "Forecast, not demand", and never stored in a plan, procurement run or purchase
  order.

## 17. Backtesting results

**No backtest has been run on real data, because there is none.** On the development database the readiness gate refuses
(0 verified weeks; all kits test-paid or demo), and the browser test confirms the refusal is recorded.

What is verified is the method itself, on synthetic series in unit tests:

- Baseline forecasts equal hand-calculated values (naive, MA(4), seasonal naive(4), SES(0.5) = 12.984375 on a known series).
- Rolling origin is strictly time-ordered: a spy method confirms each fold sees only earlier points, in order.
- MAE, WAPE, bias and MASE equal hand calculations. WAPE is undefined with zero demand and MASE is undefined on a flat history (null, not 0).
  Pooling adds errors and actuals rather than averaging ratios.
- Selection keeps naive unless it is beaten by the approved margin.

The integration test builds ten weeks of history by cloning a fixture week. It labels the payments as a non-test provider; that is test data
inside the test database only. With that history it confirms a completed run stores values, backtests with exactly one selected method, and
an ingredient projection.

## 18. Data readiness and quality controls

Fourteen checks (`/intelligence/quality`), each stored in `analytics_quality_results`:

- orders without items
- payment/refund mismatch
- succeeded payments without a time
- inventory ledger mismatch
- order/delivery state disagreement
- missing delivery outcome
- unreconciled runs
- missing source references
- duplicate readiness events
- failed consumer jobs
- test payments (warning)
- demo records (warning)
- stale analytics
- snapshot reconciliation

**Verified**: on consistent data the refund check is OK and test payments warn. After a refunded amount is edited outside the ledger,
the refund check fails with a count of 1.

## 19. RBAC and security

- 15 new permissions: `analytics_{executive,revenue,customers,production,procurement,inventory,delivery}.read`, `analytics.export`,
  `automation.{read,manage,execute,approve}` and `forecasting.{read,run,configure}`. Also a new role, **analyst** (reads every analytics
  area, forecasts and automation history, and exports; cannot configure, execute or approve).
- Grants: admin and super admin hold all of them. Finance holds revenue and executive analytics, plus automation approve. Operations,
  production, procurement, inventory and dispatch managers hold their own areas.
- Every route is behind `identity.RequireAdmin`. Dashboards check the dashboard's permission, and the metric catalogue lists only metrics the
  caller may see. **Verified**: kitchen staff are refused revenue (API 403, console 404, no nav entry); a production manager reads
  production but cannot export revenue; analysts cannot change rules, retry, decide or run forecasts.
- Exports are aggregates only, formula-neutralised (`SafeCSV`), `Cache-Control: no-store, private`, and audited. **Verified**.
- Audited actions: rule saves, evaluations, retries, decisions, task closes, report schedule changes, generation and downloads, forecast
  criteria, refused and completed runs, analytics refreshes and exports.

## 20. Database migrations

`00013_analytics_automation.sql`, with a full Down:

- **Analytics**: `analytics_refresh_runs`, `analytics_snapshots`, `analytics_quality_results`.
- **Automation**: `automation_rules` (6 seeded, disabled; CHECK: enabled requires approval and cooldown), `automation_rule_versions`,
  `automation_executions` (unique `dedupe_key`), `automation_execution_events`, `automation_tasks`.
- **Reports**: `scheduled_reports` (5 seeded, disabled), `report_runs` (unique schedule and period), `report_run_deliveries`.
- **Forecasting**: `forecast_settings` (singleton, criteria NULL), plus append-only `forecast_runs`, `forecast_values` and
  `forecast_backtests`.
- **Seed data**: consumer offsets `analytics` and `automation`, the analyst role, permissions and grants.

Configuration rows carry `updated_by` without a user foreign key, as `delivery_settings` does, so seeded configuration outlives accounts.

Applied to the development database (`goose` version 13) and the test database. **Verified** by the test harness, which migrates on every run.

## 21. API endpoints

OpenAPI `docs/api/openapi.yaml` 1.8.0: 31 new operations (29 paths, 35 schemas); the TypeScript client was regenerated with an `intelligence` namespace.
`TestRoutesMatchOpenAPI` passes (every route documented, and every documented route exists).

- **Analytics**: `GET /admin/analytics/metrics | status | data-quality | series | {kind} | {kind}/export`;
  `POST /admin/analytics/refresh | data-quality/run`.
- **Automation**: `GET /admin/automation/overview | rules | executions | executions/{id} | tasks`;
  `PATCH /rules/{id}`; `POST /rules/{id}/evaluate | executions/{id}/retry | executions/{id}/decision | tasks/{id}/close`.
- **Reports**: `GET /admin/reports/scheduled | runs | runs/{id} | runs/{id}/csv`; `PATCH /scheduled/{id}`; `POST /scheduled/{id}/run`.
- **Forecasting**: `GET /admin/forecasting/readiness | settings | runs | runs/{id} | runs/{id}/ingredients`; `PUT /settings`; `POST /runs`.

## 22. UI screenshots

`docs/screenshots/sprint-8/`, captured by `e2e/tests/intelligence.spec.ts` against the running stack:

| File | Screen |
|---|---|
| 01-executive-overview | Executive overview with the test-data notice |
| 02-revenue | Revenue dashboard, definition opened |
| 03-delivery-analytics | Delivery dashboard |
| 04-metric-catalogue | Metric catalogue |
| 05-overview-phone | Overview on a 390px phone |
| 06-data-quality | Data quality and freshness |
| 07-automation-rules | Rules, each listing the decisions it still needs |
| 08-automation-executions | Execution history |
| 09-scheduled-reports | Schedules and generated reports |
| 10-generated-report | A stored report (no recipients) |
| 11-forecast-readiness | Readiness: refused, criteria "Not decided" |
| 12-forecast-refused | A refused run |
| 13-forecast-criteria | Activation criteria, empty |

## 23. Automated test results (15 November 2026)

| Command | Result |
|---|---|
| `go vet ./...` (in `services/api`, Go 1.27 container) | clean |
| `go test ./... -count=1 -p 1` | all 16 packages `ok` (`internal/server` 191 s) |
| `pnpm test` (Vitest) | 43 passed: admin 6, web 15, marketing 22 |
| `pnpm typecheck`, `pnpm lint` | clean |
| `pnpm exec playwright test` (3 batches, `--workers=1`, after `production seed-demo`, `delivery seed-demo -ready`, `production seed-demo`) | 38 passed: 22 + 9 + 7 |

New Sprint 8 tests:

- **Go unit tests**: `TestCatalogIsUpToDate`, `TestRegistryIsComplete`, `TestSafeCSV`, `TestRangesUseLocalDays`,
  `TestBaselinesCalculateCorrectly`, `TestBacktestingUsesTimeOrderedData`, `TestEvaluationMetricsAreCorrect`,
  `TestSelectionKeepsNaiveUnlessBeaten`.
- **Go integration tests**: `TestAnalyticsReconcileWithAuthoritativeRecords`, `TestAutomationRulesAreApprovedDedupedAndVisible`,
  `TestScheduledReportsRunOncePerOccurrence`, `TestForecastingIsGatedAdvisoryAndUsesApprovedRecipes`, `TestDeliveryCostRecords`.
- **Browser** (`intelligence.spec.ts`, 3 tests, including axe WCAG 2 A/AA checks on three screens).

Brief section 45, required tests 1–30, all covered:

- 1–15 and 28–29: the analytics test and the catalogue/range unit tests.
- 16: scheduled reports.
- 17–20: automation.
- 21–22, 26–27: the forecasting integration test.
- 23–25: model unit tests.
- 30: the full regression above.

Defects found and fixed while testing:

- **Metric currency.** A metric's currency rule text was passed to the money formatter as if it were a currency code. The API now sends the
  ISO code, and the UI accepts only a code.
- **Table scrolling.** The shared admin `DataTable` scroll area could not be reached by keyboard when its cells held only text (axe
  `scrollable-region-focusable`). It is now a focusable labelled region; this applies to every admin table.

## 24. Known limitations

- All money in development is test-provider money and some orders are for demo meals. Dashboards are a working instrument, not a picture of
  the business.
- Commerce emits no events, so revenue and subscription snapshots update on the hourly tick, not instantly. Dashboards are live regardless.
- Procurement and delivery dashboard figures have no figure-level reconciliation test (sections 10 and 12).
- The `automation` event consumer path and the `misectl analytics backfill` command are not covered end to end.
- Ordered value counts orders by when they were created; kits ordered counts by order week. On a range near a week boundary the two can
  disagree; each definition says which it uses.
- Report emails are plain text and HTML with the CSV inline (capped at 20,000 characters), not an attachment. No real mail service was used.
- Forecast ingredient projections use each meal's catalogue servings, not per-order servings, and are not stored.
- The browser test switches the "Payment failed" rule on, checks it, and switches it off again. The cooldown and action it set remain in the
  development database's rule configuration (as version history).
- `gofmt` flags 14 files from earlier sprints that were not reformatted, to keep this diff focused. All Sprint 8 files are formatted.
- Existing `/reports/*` pages from earlier sprints still calculate their own figures (section 1).

## 25. Outstanding business decisions (blocked until decided)

| Decision | Blocks |
|---|---|
| Revenue recognition policy | Any "recognised revenue" figure. Only cash is shown. |
| Each rule's thresholds, cooldown, action, priority and approval | All six automation rules (off) |
| Report recipients and approval | All five scheduled reports (off) |
| A production mail service | Report emails (recorded as skipped) |
| Forecast activation criteria: minimum weeks, maximum missing share, required improvement over naive, residuals for a range, season length, approval | All forecasts (refused) |
| Real payment provider (Sprint 3 decision, still open) | Any verified demand, so forecasting cannot activate even with criteria |
| Whether free (promotion) weeks count as real demand | Currently excluded from forecasting |
| Analytics retention period for snapshots and report content | Nothing yet; data is kept |

## 26. Sprint 9 readiness

Ready to start once authorised. Recommended first items, in order:

1. Decide the items in section 25. Automation, reports and forecasting are built and waiting on decisions, not code.
2. Add outbox events to commerce (payments, subscriptions, orders) so revenue snapshots and the `payment_failed` rule react immediately.
3. Move the earlier `/reports/*` pages onto the metric registry so there is truly one definition per figure.
4. Add figure-level reconciliation tests for procurement and delivery dashboards and an end-to-end test of the automation event consumer.
5. Connect a real mail service and send report CSVs as attachments.

## Commit

Sprint 8 committed as `f2e1ce0` on `sprint-8/analytics-automation` (local; not pushed), working tree clean after the commit. This hash note was added in a follow-up commit.
