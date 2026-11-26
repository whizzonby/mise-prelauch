# Sprint 9.2 plan: Mise Intelligence

Brief: `E:\FULL MISE DEV\Sprint 9.2`. Branch `sprint-9.2/intelligence` (from `2274003`). Authorised 25 November 2026. Audit:
`SPRINT_9_2_AUDIT.md`. Hard limits: admin-only; advisory only; no fabricated data; no unapproved external AI; no Sprint 10.

Owner decisions (25 November 2026):
- **No AI provider key.** The provider interface, an OpenAI adapter (tested against a local fake), the tools, evidence validation and
  the evaluation harness are built; tests use scripted (mock) model responses; the assistant and AI narratives show "AI unavailable"
  until a key and a budget are configured. Everything deterministic works without AI.

## 1. Principles

- Deterministic Go code computes every number (finance, scenarios, segments, risks). The model only explains, summarises and
  structures, using tool results; every numeric claim it makes is checked against those results.
- Read from the authoritative tables and the Sprint 8 metric registry; never copy commerce, inventory, production or delivery data.
- Missing data is shown as missing, never as zero. Test payments and demo content are excluded from "real" figures and labelled.
- Nothing customer-facing: routes under `/api/v1/admin/intelligence/*` and `/api/v1/admin/finance/*`; admin UI only.

## 2. Management finance (`internal/finance`)

Tables: `finance_expense_categories` (11 seeded: salaries and wages, kitchen rent, utilities, packaging, marketing, insurance,
equipment maintenance, transportation, payment processing fees, professional services, other operating expenses; each with a
`cost_class`: variable, fixed, labor, marketing), `finance_expenses` (category, description, amount minor + currency, kind
actual|estimated, expense date, payment date, supporting reference, status draft|submitted|approved|rejected|void, source manual or
recurring schedule), `finance_expense_events` (append-only audit), `finance_recurring_expenses` (category, amount, cadence
weekly|monthly|quarterly|yearly, start/end, approved), `finance_coverage_confirmations` (a category confirmed as **nothing to record**
for a month, by whom: the only way a missing category becomes zero), `finance_cash_balances` (approved opening balances by date).

**Ownership rules (no double counting)** — documented in `docs/finance/FINANCIAL_METRIC_DEFINITIONS.md`:
- Collections, refunds, provider fees and settlements come only from `payments`/`refunds`. A manual "payment processing fees" expense
  is for fees providers do not report per payment (e.g. a monthly gateway fee); per-payment fees are never also entered manually.
- Ingredient **consumption** cost = inventory issue movements × lot unit cost; ingredient **purchases** = goods receipts. Contribution
  margin uses consumption; cash outflow would use supplier payments (not recorded in the platform: shown as missing).
- Delivery costs come only from `delivery_cost_records`; the "transportation" expense category is for other transport.
- Waste cost = waste movements × lot unit cost (shown separately, already inside consumption only if issued).
- Refunds reconcile with the payment ledger (`payments.refunded_minor` = sum of succeeded refunds), checked as a data-quality item.

**Financial summary** (period, currency TTD): gross collections, refunds, net collections, provider fees, ingredient consumption,
packaging, delivery, labour, marketing, fixed operating costs, contribution margin, estimated gross margin, estimated operating result,
cash inflows (settlements where reported, else collections labelled "not settlement"), cash outflows (paid expenses), recorded cash
balance. Each line carries its sources and a **coverage** state (complete, partial with the missing categories/months, unavailable).
Contribution margin, gross margin and operating result are **withheld** (null + reasons) unless every component they need is covered.
Revenue recognition: **not calculated** (no approved policy). Test-provider payments are excluded and reported separately.

## 3. Scenario engine (`internal/finance/scenario`)

Deterministic, weekly steps over 30/60/90/182/365 days. Inputs separated into **observed** (computed from records, with period and
evidence) and **assumptions** (entered or preset, labelled). Variables: active subscriptions, new subscriptions per week, weekly churn,
renewal rate, average weekly subscription value, cost per kit (ingredients, packaging, delivery), provider fee rate, fixed costs per
month, marketing per month, labour per month, capacity (kits per week), price change, ingredient cost change, one-off investments
(kitchen expansion), opening cash. Outputs per week and in total: subscribers, kits, sales, variable costs, contribution, fixed costs,
operating result, cumulative cash (if opening cash), capacity breaches; break-even subscribers (`fixed / contribution per subscriber`,
exact integer maths, rounded up). Presets: base (observed values only), conservative and optimistic (explicit multipliers shown as
assumptions; never called probable). Scenarios are versioned (`finance_scenarios`, `finance_scenario_versions` with assumptions,
observed snapshot, engine version and an input hash: the same inputs always give the same output). What-if: price increase, growth,
churn, ingredient cost, hires, kitchen expansion, delivery expansion, new meal, marketing, supplier change — all as assumption
deltas over a base. Comparison: side by side.

## 4. Behaviour (`internal/behavior`)

`behavior_events` (event id from the sender, unique → deduplicated; name; contract version; customer id or anonymous id; session id;
whitelisted properties; occurred/received). Contract v1 events: `catalogue_viewed`, `meal_viewed`, `plan_viewed`, `plan_selected`,
`meal_selected` (selection saved), `checkout_started` — recorded server-side by the customer app (BFF) so nothing is lost to ad
blockers and no token reaches the browser; skipped under GPC/DNT (same rule as `packages/analytics`). Derived from domain events (not
re-recorded): `payment_completed`, `subscription_renewed`, `subscription_paused`, `subscription_cancelled`, `support_case_created`.
**Checkout abandoned** = a `checkout_started` with no succeeded payment for that customer within 24 hours (computed). **Meal rated:**
no rating feature exists; deferred (needs a product decision).

Segments (rules, each with its definition and count; members only with `customers.read`): new (first paid week within 4 weeks), repeat
(2+ paid weeks), long-term (12+ paid weeks), inactive (no paid week in the last 4 weeks and no live subscription), renewal failures
(a failed renewal in the last 60 days), repeated delivery issues (2+ failed or disputed deliveries in 90 days), category preference
(50 %+ of 3+ selections in one category). No sensitive attributes, no churn score.

## 5. Intelligence (`internal/intelligence`)

- **Tools** (typed, read-only, each with a required permission, argument schema and validation, output with evidence references
  {metric or tool id, domain, period, filters, freshness, source refs}): business overview, financial summary, expense breakdown,
  subscription metrics, customer cohorts and segments, conversion funnel, meal performance, recipe cost summary, supplier
  performance, inventory risks, production metrics, delivery metrics, support trends, run financial scenario (stateless), demand
  forecast (latest run; "not ready" when criteria unmet), data quality and readiness, knowledge search (published help articles and
  metric definitions; FTS). The assistant receives only the tools the caller's permissions allow.
- **Provider interface**: provider/model configurable; timeout; streaming; structured output (JSON schema); tool calling; usage;
  retries; feature flag. Adapters: `openai` (Chat Completions with tools, tested against a fake server; **not verified** against
  OpenAI), `mock` (scripted, tests only). Credentials server-side only.
- **Assistant**: question → versioned prompt template → tool loop (max calls, max time, token limits) → structured answer {answer,
  claims with values and tool references, tables, chart specs, assumptions, limitations, follow-ups} → **numeric validation** (every
  number in a claim must equal a value in the tool results it cites; failures trigger one regeneration, then the answer is returned
  with the unsupported claims removed and a limitation) → stored run.
- **Risks and opportunities**: deterministic indicators with editable thresholds (defaults set by engineering, labelled), each with
  metrics, comparison period, severity, explanation, freshness and a review action; no probabilities. Opportunities labelled
  observed / AI hypothesis / business proposal / validated.
- **Meal proposals**: a deterministic evidence pack (demand by category, availability constraints, approved supplier products and
  prices, recipe costs) plus, when AI is available, draft concepts; lifecycle proposed → management review → handed to culinary
  (links to a recipe created through the existing recipe workflow) → closed/rejected. No code path from intelligence to recipe or
  meal publishing.
- **Briefings**: daily/weekly/monthly, deterministic content (changes, risks, opportunities, limitations) plus an AI narrative when
  available; stored versions with an input hash (identical inputs → no new version); schedules disabled by default with approved
  staff recipients only.
- **Recommendations**: risks, opportunities and AI suggestions stored for review: save, dismiss, assign; any action goes through
  existing workflows.
- **History, usage, budgets**: every run stored (user or job, provider, model, prompt version, tools, period, output, evidence,
  limitations, review status, tokens, estimated cost); per-user daily request limit, organisation monthly budget, token and tool-call
  caps, time limit; retention setting.
- **Readiness**: real vs test payments, expense coverage, demand history, behaviour coverage, supplier cost coverage, delivery
  completeness, forecast readiness, AI availability.

## 6. Permissions

`intelligence.read`, `intelligence.ask`, `intelligence.executive.read`, `intelligence.financial.read`, `intelligence.customer.read`,
`intelligence.operational.read`, `intelligence.scenario.run`, `intelligence.recommendation.review`, `intelligence.configure`,
`intelligence.export`, `finance.expenses.read`, `finance.expenses.manage`, `finance.expenses.approve`. super_admin: all; admin: all
except configure and expenses.approve; finance: read, ask, financial, scenario, export, expenses read/manage/approve; operations:
read, ask, operational; marketing: read, ask, customer; support and others: none.

## 7. UI (admin)

`/intelligence/command-center` (overview, briefing, finance, outlook, customers, meals, operations, risks, opportunities,
recommendations, Ask Mise AI, saved reports, history, readiness, usage) and `/finance/expenses` (+ categories, recurring, coverage,
cash balances). Existing navigation, tokens and chart components; every metric with definition and freshness; every recommendation
with its evidence.

## 8. Testing

Go integration tests on PostgreSQL for every brief §39 item (deterministic finance, no double counting, withheld profit, settlement vs
collection, currency, break-even, reproducible scenarios, event dedupe, explainable segments, demo isolation, proposals with evidence,
AI cannot publish or act, restricted metrics, prompt injection, numeric reconciliation, limitations, provider failure, idempotent
briefings, budgets, admin-only access); mocked model responses; an evaluation dataset run against the mock now and a real provider
later. Playwright for the command center, expenses, scenarios and assistant (unavailable state) on desktop and phone; full regression.

## Progress

Resume from the first unchecked item.

- [x] Stage 1 — Audit (`SPRINT_9_2_AUDIT.md`), this plan
- [ ] Stage 2 — Finance: migration, expenses + approval + audit, recurring, coverage, cash balances; financial summary; tests
- [ ] Stage 3 — Behaviour events (contract v1, BFF recording, dedupe, abandonment) and segments; tests
- [ ] Stage 4 — Intelligence tools and evidence contracts; readiness; tests
- [ ] Stage 5 — AI provider interface (OpenAI adapter + mock), assistant with validation, history, usage and budgets; tests
- [ ] Stage 6 — Scenario engine, presets, what-if, comparison, versions; tests
- [ ] Stage 7 — Meal proposals; operational intelligence; risks and opportunities; recommendations; tests
- [ ] Stage 8 — Briefings; command center and finance UI
- [ ] Stage 9 — Docs, security and AI evaluation, Playwright, regression, screenshots, report, commit
