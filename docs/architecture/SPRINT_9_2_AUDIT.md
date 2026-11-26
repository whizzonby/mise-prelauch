# Sprint 9.2 audit: what Mise Intelligence can build on

Audited 25 November 2026 on branch `sprint-9.2/intelligence` (from `2274003`), against the code and the development database,
not earlier reports. Plan: `SPRINT_9_2_PLAN.md`.

## 1. Available business metrics

`internal/analytics/metrics.go` registers **62** metrics (Sprint 8's 52 plus Sprint 9.1's 10 support metrics), each with a
definition, sources, calculation, filters, missing-data rule, owner, permission and SQL. Domains: revenue 9, subscriptions 7,
customers 3, orders 2, meals 1, production 9, procurement 6, inventory 6, delivery 8, operations 1, support 10. Dashboards per
domain, daily snapshots, refresh runs and data-quality checks (`quality.go`) exist; every metric is gated by a domain permission
(`analytics_*.read`). **The intelligence layer reads these through the registry; it does not re-derive them.**

## 2. Financial records (and what is missing)

| Area | Records that exist | Authoritative for |
|---|---|---|
| Customer billing | `billing_cycles`, `orders`, `invoices`, `invoice_items` (integer minor units, one currency per document) | What was billed |
| Collections | `payments` (status, `amount_minor`, `refunded_minor`, provider; Sprint 9.1: `charged_*`, `settlement_*`, `fx_*`, `fee_*`), `refunds` | Money collected, refunded, provider fees and settlement **where the provider reports them** |
| Purchasing | `purchase_orders`, `purchase_order_lines`, `purchase_order_revisions`, `quotations`, `supplier_prices` | What was ordered and at what price |
| Receiving | `goods_receipt_lines.unit_cost_minor` | What was received and its cost |
| Inventory | `inventory_lots.unit_cost_minor`, `inventory_movements` (types incl. issue and waste) | Stock value; consumption and waste can be costed from lot costs |
| Recipes | `ingredient_cost_references` | Reference ingredient costs for costing |
| Delivery | `delivery_cost_records` (partner, delivery/run, kind, amount, status) | Delivery costs |

**Missing entirely:** operating expenses (salaries, rent, utilities, marketing, insurance, maintenance, professional services),
supplier invoices, supplier payments (cash out to suppliers), payroll, packaging costs, cash/bank balances, a revenue recognition
policy. Without these, profitability and cash flow can only be **partial**, and must be shown as such.

## 3. Customer behaviour data

| Source | What it records |
|---|---|
| `lead_events` (marketing site, `packages/analytics`) | Anonymous first-party events: `page_view`, `section_viewed`, `hero_cta_clicked`, waitlist steps, `faq_opened`, `meal_viewed`, `plan_viewed`, referral events. Fixed taxonomy enforced by the API; no cookies; nothing sent under GPC or DNT. Dev data: 928 page views, 54 plan views, 17 meal views. |
| `domain_events` (outbox) | Business facts: `subscription.*` (created, activated, renewed, renewal_failed, paused, pause_scheduled, resumed, cancel_scheduled, cancelled, expired, plan_changed, week_unpaid, payment_recovered), `payment.*`, `order.*`, production, procurement, fulfilment, delivery, recipe events. |
| Subscriptions | `cancel_reason` recorded when a customer cancels (Sprint 3 reasons list). |
| Support | `support_cases` (category), chat conversations and feedback (Sprint 9.1). |

**Missing instrumentation (customer app):** no events for catalogue viewed, meal detail viewed, plan viewed/selected, meal selected,
checkout started; no derived checkout abandonment; **no meal rating feature exists** (so "meal rated" cannot be instrumented until a
rating feature is approved and built).

## 4. Forecasting

`internal/forecasting`: weekly meal demand forecasts from confirmed orders with configurable readiness criteria (minimum weeks,
missing share, improvement over a naive model, residuals for intervals). **Ships with criteria unset, so it never forecasts until the
business sets them.** Ingredient projections from forecast runs exist. There is no revenue or cash forecasting.

## 5. Automation and reporting

`internal/automation`: approved rules → tasks and notifications only (no automatic refunds, production or purchasing changes);
executions with approval; `scheduled_reports` with recipients and an approval reference, disabled by default; `report_runs` store
generated content. **Executive briefings reuse this pattern** (approved schedules and recipients, stored versions).

## 6. Data quality and current data

All 328 payments in the development database are from the **test** provider (`testpay`); orders span 46 "weeks" because end-to-end
tests move the commerce clock into 2027; the 45 non-demo meals were created by tests. Demo marking exists on meals, categories,
plans, chefs and delivery partners/couriers (`is_demo` / `demo`); test payments are identified by provider. **There is no real
business history.** Intelligence must say so (readiness), never present test-data patterns as trends, and keep demo/test data out of
anything labelled as real.

## 7. Existing AI functionality

None. No provider SDK, no model calls, no prompt or tool code anywhere in the repository. **No AI credentials exist** (owner
decision, 25 Nov 2026): the provider is built and tested against mocked responses and shown as unavailable.

## 8. Permissions and architecture

133 permissions; analytics per domain (`analytics_executive.read`, `_revenue`, `_customers`, `_production`, `_procurement`,
`_inventory`, `_delivery`), `analytics.export`, `automation.*`, `forecasting.*`. Admin endpoints use `RequireAdmin(perm)`; the RBAC
matrix test reads route permissions from source. Jobs: PostgreSQL queue with retries; outbox consumers with offsets. The admin app is
a BFF (tokens server-side). Migrations end at `00016`.

## 9. Security risks for an AI layer

| Risk | Present today? | Plan |
|---|---|---|
| Model given broad data access | No AI yet | Read-only typed tools only, filtered by the caller's permissions, no SQL, argument validation. |
| Prompt injection via customer text (chat messages, support notes, cancellation free text, help articles) | Customer text exists in the database | Tools return aggregates, not raw customer text; any retrieved text is wrapped as untrusted data; tool permissions are enforced in Go, not by instructions. |
| Personal data sent to an external provider | — | Aggregates only; no names, emails, addresses or message bodies in prompts; documented. |
| Fabricated numbers | — | Every numeric claim in an answer must match a tool result value or the answer is rejected or limited. |
| Cost runaway | — | Per-user request limits, organisation budget, token and tool-call caps, time limit. |
| Customers reaching intelligence | — | Admin-only routes under `/api/v1/admin/intelligence` and `/admin/finance`; tests for customers, guests and staff without permission. |

## 10. Proposed architecture

Three new modules in the monolith, reading existing modules through their services and SQL over authoritative tables (no copies):

- `internal/finance` — expense categories, expenses (actual/estimated, approval, audit), recurring schedules, opening cash
  balances, coverage confirmations; deterministic financial summary and cash view; the scenario engine.
- `internal/behavior` — versioned first-party customer-app events (deduplicated), derived checkout abandonment, transparent segments.
- `internal/intelligence` — read-only tool registry with evidence references, the model-provider interface (OpenAI adapter + mock),
  the assistant with numeric-claim validation, risks, opportunities, meal proposals, briefings, recommendations, history, usage and
  budgets, readiness.

Details, decisions and stages: `SPRINT_9_2_PLAN.md`.
