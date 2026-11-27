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
- [x] Stage 2 — Finance: migration, expenses + approval + audit, recurring, coverage, cash balances; financial summary; tests — 25 Nov: migration 00017 (permissions renamed `finance_expenses.*`: the database requires `domain.action`); `internal/finance` (expenses.go, recurring.go, summary.go, http.go); 13 admin routes in OpenAPI + client. Tests `finance_test.go`: coverage honesty (results withheld while any input is missing; test payments excluded; partial fees withhold contribution), exact deterministic results, settlement vs collections, cash balance approval, other currency excluded, workflow guards (own-approval, edit after approval, void needs a note), immutable history, permissions; recurring estimate replaced by the actual (never counted twice); month-end clamping.
- [x] Stage 3 — Behaviour events (contract v1, BFF recording, dedupe, abandonment) and segments; tests — 21 Nov: migration 00018 (`behavior_events` + the 10 intelligence permissions as `intelligence.*` / `intelligence_<domain>.*`); `internal/behavior` (contract validation, dedupe by event_id, funnel with ledger payments, 24-hour abandonment, cancellation reasons, 7 rule-based segments; members need customers.read); customer app relay `/api/behavior`, `lib/behavior.ts` (GPC/DNT, no cookies), `<Track>` on catalogue, meal, plan, subscribe, review, and `meal_selected` on save; docs/analytics/BEHAVIOR_EVENTS.md. Tests `behavior_test.go` (contract refusals, dedupe, privacy signals, attribution, funnel/abandonment, reasons, segments, access); live browser check: one event per view, only identifiers, nothing under GPC. Full Go suite passes.
- [x] Stage 4 — Intelligence tools and evidence contracts; readiness; tests — 21 Nov: `internal/intelligence` (tools.go: 17 typed read-only tools over analytics dashboards, finance, behaviour, forecasting and a metric/help knowledge search; each needs its domain permission AND the permission of the data it reads; facts with null = not available; evidence {ref, tool, domain, period, filters, freshness, sources}; arguments outside a tool contract refused. readiness.go: payments, expense coverage, demand history, forecasting, behaviour, ingredient costs, delivery costs, AI). Routes GET tools, POST tools/{name}, GET readiness (OpenAPI + client; matrix exception for tools/{name}). Tests `intelligence_test.go` (tool sets per role, refusals, every tool runs with evidence, overview = dashboard numbers, test payments not revenue, withheld results, readiness, knowledge search). Dev data readiness checked: overall not ready (294 test payments, no expenses, no verified demand, AI off).
- [x] Stage 5 — AI provider interface (OpenAI adapter + mock), assistant with validation, history, usage and budgets; tests — 20 Nov
  (PC clock): migration 00019 (`intelligence_ai_settings`: off, no budget, no prices as shipped; `intelligence_runs`). `internal/intelligence/ai`
  (Provider interface, Chat Completions adapter with tools, strict JSON-schema output, SSE streaming, usage, retry policy that never
  retries a streamed reply or exhausted quota; scripted Mock). Config `AI_PROVIDER`/`AI_MODEL`/`AI_API_KEY`/`AI_TIMEOUT`/`AI_RETRIES` (model is
  configuration only). Assistant (`assistant.go`): prompt `ask-v1`, only the caller's tools offered, every call re-checked by the toolbox,
  tool output bounded (40 rows, 300-char cells) and scrubbed of emails/phones, caps on tool calls, model calls, input tokens and seconds;
  structured answer {answer, claims, tables, charts, assumptions, limitations, follow_ups}; numeric validation (`validate.go`: numbers must
  be tool values, rounded or in major units/percent; citations must be refs retrieved in this run; one regeneration, then unsupported
  parts removed and marked `[unverified]`, status partial); tool limitations always carried into the answer; failed runs stored with no
  answer. Status: unavailable unless provider + owner switch + budget + prices + month budget left. Settings need budget, prices, their
  source and an approval ref to turn on (audited). Per-user daily limit, monthly budget, estimated cost (rounded up). History: own runs
  (all for intelligence.configure), review (flag needs a note), worker purge by retention. Routes GET ai, PUT ai/settings, POST ask, GET
  runs, GET runs/{id}, POST runs/{id}/review (OpenAPI + client). Tests: ai_test.go (fake server: tools, schema, streaming, cut stream,
  retry/no-retry, timeout, missing config), validate_test.go, intelligence_ai_test.go (unavailable without provider; settings and
  availability; provider failure; answer with evidence, usage and cost; fabricated number/citation regenerated then removed; history
  scope and review; prompt injection cannot reach finance tools, unknown tools or extra arguments; tool-call cap; daily limit; budget;
  retention). Streaming is in the adapter; the ask endpoint returns the validated answer only (unvalidated numbers are never shown).
- [x] Stage 6 — Scenario engine, presets, what-if, comparison, versions; tests — 27 Nov (real date): `internal/finance/scenario`
  (engine.go: 16 inputs, exact `math/big` arithmetic, weekly steps over 30/60/90/182/365 days, half-away-from-zero rounding per week,
  totals = sums of the rounded weeks, drift-free fixed costs, capacity breaches, cash view only with an opening balance, peak cash need,
  break-even subscriptions rounded up, engine version `scenario-v1`, SHA-256 input hash; nothing projected while a required input is
  missing. whatif.go: presets base/conservative/optimistic as labelled multipliers ("not probabilities"), 10 what-if kinds as assumption
  deltas, refused on missing inputs or unknown params. service.go: observed inputs over the last complete calendar months (real
  subscriptions = a real paid billing cycle; costs only from complete finance lines; per-kit costs divide by all paid kits since test
  orders used real ingredients; nil with the reason otherwise; capacity never observed), resolve order observed → preset → entered →
  what-if, saved scenarios and immutable versions (identical inputs return the existing version), rerun = stored output, compare 2-4,
  archive; audited). Migration 00020. 9 routes under `/admin/intelligence/scenarios*` and `scenario-versions/{id}` (intelligence_scenario.run;
  OpenAPI + client); AI tool `run_financial_scenario` (18 tools). Tests: engine_test.go (hand arithmetic, reproducibility, hash, missing
  inputs, drift, cash, capacity, no break-even, rounding, validation, presets, what-ifs) and intelligence_scenario_test.go (observed
  missing-not-zero, incomplete run, full run, refusals, save/dedupe/version/reproduce/compare/archive/audit, access by role, the AI tool,
  test-paid subscriptions not counted).
- [x] Stage 7 — Meal proposals; operational intelligence; risks and opportunities; recommendations; tests — 27 Nov (real date):
  migration 00021 (risk thresholds seeded as engineering defaults, recommendations + events, meal proposals + events). meals.go: per meal
  weeks on a published menu, kits ordered, kits per week on menu (availability-adjusted), repeat customers, waste records, recipe cost per
  serving (culinary reference costing), rule labels high_demand / availability_constrained / underperforming / high_cost (median rules,
  stated); ratings and per-meal margin reported as not available. risks.go: 11 indicators (renewals, payment failures, refunds, ingredient
  costs, contribution margin, expired stock, supplier delays, waste, delivery failures, incomplete financial records, cash via the base
  scenario) with value, comparison period, metrics, threshold, severity, explanation, review action, freshness; not_evaluable with the
  reason when data is missing; filtered by the caller's domain and data permissions; thresholds editable (configure, reason, direction
  check, audited). opportunities.go: observed only (meal availability, meal review, meal cost, supplier price rises ≥ 10 %, retention
  segments) with a caution. recommendations.go: from risk/opportunity (recomputed server-side, idempotent per open item), business
  proposals, review actions save/assign/dismiss/done/validate (basis observed → validated), domain-scoped. proposals.go: evidence pack,
  manual and AI drafts (concepts-v1; numbers not in the evidence marked [unverified]; stored run kind meal_concepts), lifecycle proposed →
  in_review → approved_for_development → in_development (links an existing recipe) → closed / rejected; 6 required culinary approvals;
  no cost or nutrition/allergen claims. AI tools meal_intelligence and business_risks (20 tools; the principal travels in the tool context).
  17 routes (OpenAPI + client; RBAC scope notes for meals and generate). Tests intelligence_insights_test.go (risk explanations, domain
  filtering, idempotent save, thresholds, tool parity; recommendations scoped, review flow, nothing operational changed; meal intelligence;
  proposals lifecycle, no meal/recipe changed, AI unavailable; AI concepts with an invented number marked and a malformed concept skipped).
- [ ] Stage 8 — Briefings; command center and finance UI
- [ ] Stage 9 — Docs, security and AI evaluation, Playwright, regression, screenshots, report, commit
