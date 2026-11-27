# Tool contracts

Sprint 9.2 (`internal/intelligence/tools.go`). The 20 typed, **read-only** tools Mise Intelligence exposes to the language model and to
the console. There is no SQL tool and no write tool. List the caller's tools with `GET /api/v1/admin/intelligence/tools`; run one with
`POST /api/v1/admin/intelligence/tools/{name}` `{"args": {...}}`.

## Contract

- **Access**: a tool needs the intelligence permission of its domain **and** the permission of the data it reads. It is offered to the
  model only when both are held, and checked again on every call (also when the model asks for it by name).
- **Arguments**: strings only, at most 200 characters, only the names the tool lists (JSON schema with `additionalProperties: false`);
  anything else is refused (`422`). Periods are `from`/`to` (YYYY-MM-DD, local dates, at most two years).
- **Result**: `{tool, title, evidence, facts[], tables[], limitations[], notes[]}`.
  - `facts`: `{key, label, value, unit, currency, definition, note}`. `value: null` means **not available** (missing or withheld, with
    the reason in `note` or `limitations`), never zero. Units: count, money_minor, ratio, number, hours, days, basis_points.
  - `evidence`: `{ref, tool, domain, from, to, filters, freshness, sources[]}`; `ref` is `E1`, `E2` … within an AI run (`direct` for a
    console call).
  - `limitations`: what the result cannot be relied on for (test payments, withheld results, missing data). They are always carried into
    an AI answer.

## Tools

| Tool | Domain | Also needs | Arguments | What it returns |
|---|---|---|---|---|
| business_overview | executive | analytics_executive.read | from, to | Overview dashboard: money (cash view), customers, subscriptions, operations, with previous period |
| revenue_and_subscriptions | financial | analytics_revenue.read | from, to, plan | Ordered, charged, collected, refunded, outstanding; failed payments; starts, renewals, pauses, cancellations |
| financial_summary | financial | finance_expenses.read | from, to | Management summary lines and results (withheld with reasons), cash view, test payments apart |
| expense_breakdown | financial | finance_expenses.read | from, to | Expenses by category with coverage and missing months |
| run_financial_scenario | financial | intelligence_scenario.run | preset, horizon_days | The scenario engine on observed values; inputs with sources; totals and break-even, or the missing inputs |
| customer_retention | customer | analytics_customers.read | from, to | New and first-paid customers, repeat rate, cohorts, retention signals (not predictions) |
| customer_segments | customer | — | — | Rule-based segment counts with definitions (no names) |
| conversion_funnel | customer | — | from, to | People per funnel step, checkout abandonment (24-hour rule), limitations |
| cancellation_reasons | customer | — | from, to | Cancellation reasons chosen; missing ones counted as not recorded |
| meal_performance | operational | analytics_production.read | from, to, plan, meal | Meal dashboard: kits ordered with availability beside demand |
| meal_intelligence | operational | analytics_production.read | from, to | Availability-adjusted demand, repeat customers, waste records, recipe cost per serving, labels and their rules |
| production_metrics | operational | analytics_production.read | from, to, meal | Planned, produced, accepted, rejected, reworked kits; completion; holds; waste |
| supplier_performance | operational | analytics_procurement.read | from, to, supplier | PO value and count, cycle time, on-time rate, rejected packs, overdue orders (no ratings) |
| inventory_status | operational | analytics_inventory.read | from, to | Stock value, lots without cost, quarantined and expired lots, movements |
| delivery_metrics | operational | analytics_delivery.read | from, to, zone | Scheduled, delivered, failed, first-attempt and on-time rates, returns, disputes, cost |
| support_trends | operational | support_chat.analytics | from, to | Conversation volume, backlog, response and resolution hours, feedback |
| demand_forecast | operational | forecasting.read | — | Forecast readiness against its criteria; the latest run when one exists |
| data_readiness | general | — | — | Readiness checks: real vs test payments, expense coverage, demand history, behaviour, costs, forecasting, AI |
| business_risks | general | (each risk's own permissions) | from, to | The risk indicators the caller may see: value, thresholds, severity, explanation, review action |
| knowledge_search | general | — | query (required) | Metric definitions and published help-centre articles (PostgreSQL full-text search) |

Knowledge search uses PostgreSQL full-text search; pgvector is not used (no demonstrated need). Retrieved articles are data, never
instructions.

## Adding a tool

Add it to `tools` in `tools.go` with its domain, `Requires`, `Args` and run function; return facts with null for missing values and
limitations for anything the figures cannot support; add it to the tool-set expectations in `intelligence_test.go`, the evaluation
dataset if relevant, and this table.
