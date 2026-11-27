# Sprint 9.2 report: Mise Intelligence

Branch `sprint-9.2/intelligence` (from `2274003`). Authorised 25 November 2026; completed 27 November 2026 (real date: the development
PC's clock runs about a week behind, so some file and log dates read 20–21 November). Commit: `fb4fd5e` (end of this report).

**Outcome.** Mise Intelligence is built and verified as an **admin-only**, **advisory** executive system inside the existing console:
management finance, a deterministic scenario engine, first-party behaviour events and segments, meal and operational intelligence,
explainable risks and observed opportunities, reviewable recommendations and meal proposals, executive briefings, and Ask Mise AI with
permissioned read-only tools and numeric validation. **External AI is blocked**: no provider, model, key or budget is approved (owner
decision, 25 November), so AI shows "AI unavailable" everywhere and every deterministic view works without it. **Predictive and profit
figures stay withheld** on the development data, which holds only test-provider payments and no expenses; nothing is fabricated.

Status words used below: **Verified** (exercised by automated tests or a browser run, with results here), **Implemented, unverified**
(built and unit-tested against fakes but not against the real external system), **Blocked** (needs an approval or credential),
**Deferred** (needs a business or product decision).

## 1. Repository audit

`docs/architecture/SPRINT_9_2_AUDIT.md` (25 November, against code and the development database). Findings that shaped the sprint:
62 registered metrics to build on, not re-derive; operating expenses, packaging, payroll, supplier payments, cash balances and a
revenue-recognition policy **missing entirely**; no customer-app behaviour events and no meal-rating feature; forecasting ships with
criteria unset; all development payments are test-provider payments and there is no real business history; no AI code or credentials
anywhere. Plan and stage-by-stage progress: `docs/architecture/SPRINT_9_2_PLAN.md`.

## 2. Intelligence architecture

`docs/architecture/AI_INTELLIGENCE.md`. Deterministic Go computes every number (`internal/finance`, `internal/finance/scenario`,
`internal/behavior`, `internal/intelligence`); the model only explains tool results and every number it writes is checked. Admin UI:
`/intelligence/command-center` (15 tabs, each shown only to permitted roles), run / proposal / scenario detail pages,
`/finance/expenses`. Worker: scheduled briefings and AI-history retention. 58 new admin routes (444 in all) plus the customer app's
behaviour relay; migrations 00017–00022.

## 3. Financial management implementation — Verified

`docs/finance/MANAGEMENT_FINANCE.md`. 11 expense categories with cost classes and ownership rules; expenses (actual or estimated,
expense and payment dates, reference) with draft → submitted → approved / rejected, void with a reason, no self-approval except a super
admin, immutable history and audit entries; recurring costs counted as estimates until replaced by the actual (never both); coverage
confirmations (the only way a missing category becomes zero); approved cash balances. Tests: `finance_test.go` (coverage honesty,
exact results, settlement vs collections, cash balances, other currencies excluded, workflow guards, immutable history, permissions,
recurring replacement, month-end clamping); Playwright expense flow.

## 4. Financial calculations — Verified

`docs/finance/FINANCIAL_METRIC_DEFINITIONS.md`. Collections, refunds, fees, ingredient consumption and waste, delivery costs, expense
categories; contribution margin, estimated gross margin and estimated operating result **withheld with reasons** while any input is
missing; recognised revenue never calculated (no policy); test-provider payments reported apart and never revenue; cash view that does
not equate collections with bank cash.

## 5. Scenario simulator — Verified

Exact rational arithmetic, weekly steps over 30 / 60 / 90 days, six or twelve months; 16 inputs each labelled observed, preset,
assumption or what-if; **nothing projected while a required input is missing**; break-even subscriptions (rounded up); cash view only
with an opening balance; capacity breaches; presets as labelled multipliers ("not probabilities, not prediction intervals"); 10
what-if kinds; saved immutable versions with an input hash (identical inputs keep the version; a stored version re-runs to its stored
output, checked on every view); side-by-side comparison. Tests: `engine_test.go` (hand-computed arithmetic, reproducibility, hash,
missing inputs, drift-free fixed costs, cash, capacity, rounding, presets, what-ifs), `intelligence_scenario_test.go`; Playwright
(break-even 11 from entered assumptions, save, reproduced).

## 6. Customer behaviour instrumentation — Verified

`docs/analytics/BEHAVIOR_EVENTS.md`. Contract v1 (`catalogue_viewed`, `meal_viewed`, `plan_viewed`, `plan_selected`, `meal_selected`,
`checkout_started`) recorded server-side by the customer app; sender event ids deduplicate; identifiers only; nothing under Global
Privacy Control or Do Not Track; no cookies. Business facts (payments, renewals, pauses, cancellations, support cases) are read from
their own records, not re-recorded. Tests: `behavior_test.go` and a live browser check (one event per view, nothing under GPC).

## 7. Customer intelligence — Verified

Funnel (distinct people per step, 24-hour checkout-abandonment rule, real payments), cancellation reasons, 7 rule-based segments with
definitions and counts (members only with `customers.read`; no sensitive attributes, no churn scores). Retention opportunities from
segments (failed renewals, inactive, repeated delivery issues).

## 8. Meal-kit recommendation engine — Verified (deterministic) / Blocked (AI drafts against a real provider)

Meal intelligence: kits per week on a published menu (availability-adjusted, so a rarely offered meal is not read as unpopular),
repeat customers, waste records, recipe cost per serving, labelled rules (high demand, offered too rarely, underperforming, high
cost). Ratings and per-meal margin are reported as not available (no rating feature; meals are not priced individually). Meal
proposals: managers' drafts, and AI drafts (`concepts-v1`) from a deterministic evidence pack with any number not in the evidence
marked `[unverified]`; lifecycle proposed → in review → approved for development → in development (links a recipe created through the
culinary workflow) → closed / rejected, with the six required culinary approvals listed; no nutrition, allergen, cost or price claims;
**no code path publishes a meal or recipe**. Tests: `intelligence_insights_test.go` (lifecycle, meals and recipes unchanged, AI
unavailable), `TestAIMealConceptsAreDraftsWithCheckedNumbers` (scripted model); Playwright.

## 9. Operational intelligence — Verified

Eleven explainable risk indicators (renewals, payment failures, refunds, ingredient costs, contribution margin, expired stock,
supplier delays, waste, delivery failures, incomplete financial records, cash through the base scenario), each with its measure,
period and comparison period, the figures behind it, a threshold (engineering defaults, labelled; editable by `intelligence.configure`
with a reason, audited), severity, explanation and review action; "not evaluated" with the reason when data is missing; **no
probabilities**. Observed opportunities with evidence and a caution. Recommendations: saved from risks / opportunities (recomputed
server-side, idempotent per open item) or proposed by people; save, assign, dismiss (reason), done, validate (reason); bases observed,
AI hypothesis, business proposal, validated; filtered by the viewer's domains. Operations tab links the production, procurement,
inventory and delivery dashboards.

## 10. AI executive assistant — Verified with a scripted model; real model Blocked

Ask Mise AI (`ask-v1`): only the caller's tools, every call re-checked, bounded and scrubbed tool output, caps on tool calls, model
calls, tokens and time; structured answer (answer, claims with evidence refs, tables, charts, assumptions, limitations, follow-ups);
numeric validation with one regeneration, then removal and `[unverified]`; tool limitations always carried into the answer; every run
stored with provider, model, prompt version, tools, evidence, validation, tokens and estimated cost; review (reviewed / flagged).
Briefings (daily / weekly / monthly) with deterministic content, idempotent versions, an optional AI narrative once per version, and
schedules that are off until configured with executive-reader recipients and an approval. The console shows "AI unavailable" with the
reason; nothing is invented.

## 11. AI provider integration — Implemented, unverified (against OpenAI) / Blocked

`internal/intelligence/ai`: a `Provider` interface (model, timeout, streaming, structured JSON-schema output, tool calling, usage,
retry policy, errors) and an OpenAI Chat Completions adapter, tested against a local fake server (tool calls, strict schema,
streaming, cut streams, retries only for transient errors, no retry for quota or bad requests, timeouts, missing configuration).
**Not verified against OpenAI**: no key was provided. The model is configuration (`AI_MODEL`), never code. Activation needs a
provider, an owner's switch, a budget, prices with their source and an approval reference.

## 12. Evidence and tool contracts — Verified

`docs/ai/TOOL_CONTRACTS.md`: 20 typed, read-only tools; each needs its domain permission and the permission of the data it reads;
arguments outside the contract refused; facts with `null` for not available; evidence `{ref, tool, domain, period, filters, freshness,
sources}`. Tests: `intelligence_test.go` (tool sets per role, refusals, every tool returns evidence, overview equals the dashboard,
test payments not revenue, withheld results, readiness, knowledge search).

## 13. AI security — Verified

`docs/security/AI_SECURITY.md`. Admin-only at every layer (`TestMiseIntelligenceIsAdminOnly`: every intelligence and finance route
refuses guests (401), customers (401/403) and staff without intelligence permissions (403); `TestCustomerFacingAppsNeverCallIntelligence`;
the RBAC matrix; Playwright role scoping). Prompt injection cannot reach other tools, unknown tools or extra arguments
(`TestAskMiseAICannotReachToolsOutsideThePermissions`, evaluation); fabricated numbers and citations are removed; there is no write tool
and the advisory tests confirm purchase orders, campaigns, refunds, meals and recipes are untouched.

## 14. Data privacy — Verified (controls) / Deferred (provider terms)

Only aggregates reach a provider; output is bounded and scrubbed of emails and phone numbers; customer-level data, addresses and
payment data are never sent; runs are kept for the retention setting (default 180 days) and purged by the worker; tool results are not
stored. Behaviour events hold identifiers only and respect GPC/DNT. **Provider data-processing terms** (no training on inputs,
retention, region) must be confirmed before activation (`AI_SECURITY.md`).

## 15. AI evaluation results

`docs/ai/MODEL_EVALUATION.md`, dataset `docs/ai/evaluation_dataset.json` (18 cases), results `docs/ai/evaluation_results_scripted.json`.

| Metric | Threshold | Scripted model, 27 Nov |
|---|---|---|
| Authorization enforcement | 100 % | 100 % |
| Numeric accuracy (claims re-reconciled by re-running the cited tool) | 100 % | 100 % |
| Evidence correctness | 100 % | 100 % |
| Unsupported claims shown | 0 | 0 |
| Adversarial detection (forbidden tools, injected number, fake citation) | 100 % | 100 % |
| Tool selection accuracy | ≥ 90 % | 100 % |
| Refusal when data is missing | ≥ 95 % | 100 % |
| Latency p50 / p95 | p95 ≤ 20 s (real provider) | 9 / 49 ms (no network) |
| Cost per request | ≤ US$0.05 (real provider) | US$0.0083 (test prices) |

This verifies the guardrails, not a model. **The real-provider evaluation is Blocked** until a provider is approved; the same harness
runs it (`EVAL_API_BASE`, `EVAL_API_KEY`, `EVAL_MODEL`), and the owner's sign-off is in `MODEL_EVALUATION.md`.

## 16. AI usage and cost management — Verified

`docs/ai/COST_MANAGEMENT.md`. Owner settings (off, no budget and no prices as shipped): monthly budget, prices and their source,
questions per person per day, tool calls, output and input tokens, seconds, retention, approval reference (audited). Estimated cost
per run (rounded up), monthly usage and the user's day shown in Settings. Budget exhaustion makes AI unavailable; the daily limit
returns `429`. Briefings deduplicated; one narrative per version. Tests: `TestAskMiseAILimitsAndBudget`, settings tests.

## 17. Database migrations

| Migration | Contents |
|---|---|
| 00017 management finance | categories (seeded), expenses + events, recurring, coverage confirmations, cash balances; `finance_expenses.*` permissions |
| 00018 behaviour and intelligence | `behavior_events`; the 10 intelligence permissions and role grants |
| 00019 AI | `intelligence_ai_settings` (off), `intelligence_runs` |
| 00020 scenarios | `finance_scenarios`, `finance_scenario_versions` |
| 00021 insights | risk thresholds (defaults), recommendations + events, meal proposals + events |
| 00022 briefings | briefings (versions, hash, narrative), schedules (off), deliveries |

All have down migrations. Applied to the development database (`schema is at version 22`).

## 18. API contracts

`docs/api/openapi.yaml` (tags `admin-intelligence`, `admin-finance`), checked against the served routes by `TestRoutesMatchOpenAPI`;
the API client (`packages/api-client`) has `mi` and `finance` blocks typed straight from the OpenAPI operations, with compile-time
checks that they resolve. A gap found this sprint: the expense and recurring-cost transitions read a `note` the contract did not
document; added.

## 19. Desktop and mobile screenshots — Verified

`docs/screenshots/sprint-9-2/`: every command-center tab and the finance pages at 1440 × 900 and 390 × 844 (`e2e/tools/shots-9-2.mjs`,
which also checks for page errors and horizontal scrolling: none), plus Playwright captures (`e2e-*.png`: briefing, scenario run,
proposal, phone). Review of the first captures led to fixes: the overview showed 30 tiles (each figure twice); it now leads with eight
headline figures with their comparisons and keeps the rest behind "All figures"; its subtitle wrongly said test payments were excluded
(these operational figures include them; only the Finance tab excludes them); risk explanations lost internal names and odd units;
optional scenario inputs are no longer flagged as missing.

## 20. Automated test results

Run 27 November 2026 against the local stack (API, worker, PostgreSQL and Mailpit in Docker project `mise-s1`; admin, customer app and
marketing site as production builds).

| Suite | Command | Result |
|---|---|---|
| Go (unit and integration on PostgreSQL) | `go test ./... -count=1 -timeout 25m` (services/api) | **22 packages ok**; **244** test functions (199 at the end of Sprint 9.1) |
| Go static | `go vet ./...`; `gofmt -l .` | clean; clean (10 new files reformatted, whitespace only) |
| RBAC matrix | `TestEveryRoleReachesExactlyTheAdminEndpointsItIsGranted` | 26 roles × 444 admin routes = **10,656** calls, pass |
| API contract | `TestRoutesMatchOpenAPI` | pass |
| AI evaluation | `TestModelEvaluation` | every threshold met (§15) |
| TypeScript unit | `pnpm test` | Vitest **45** passed (admin 7, web 15, marketing 23) |
| Typecheck / lint | `pnpm -r typecheck`; `pnpm -r lint` | clean in every workspace |
| Dependencies | `pnpm audit --prod` | no known vulnerabilities |
| Playwright, Chromium, launch mode | 17 spec files in 3 batches, `--workers=1` | **80 passed, 3 failed, 5 skipped** (below) |
| Playwright, Chromium, waitlist mode | `waitlist experience launch` (marketing built with `NEXT_PUBLIC_SITE_MODE=waitlist`) | **25 passed, 0 failed** |
| Playwright, new spec cross-browser | `mise-intelligence.spec.ts` on Firefox, WebKit desktop, WebKit iPhone 13 size | **24 passed** |

**Failed and skipped, explained.** The 3 launch-mode failures are `experience.spec.ts` tests that exercise the waitlist page (mobile
navigation, reduced motion, keyboard-only waitlist); they are waitlist-mode tests, as in Sprint 9.1, and all pass in the waitlist-mode
run. The 5 skipped are `performance.spec.ts` and `screenshots.spec.ts`, which run only with `E2E_PERF=1`. One run is **not counted**:
an accidental `pnpm -r run test` also started the whole Playwright suite fully in parallel on this low-memory PC; it had failures in
specs that pass in the proper one-worker batches (accounts, staff invitations), so it is reported here and excluded. `govulncheck` was
not run this sprint (it was clean in Sprint 9.1; no Go dependencies were added in 9.2).

New in Sprint 9.2: Go tests in `engine_test.go`, `ai_test.go`, `validate_test.go`, `behavior_test.go`, `finance_test.go`,
`intelligence_test.go`, `intelligence_ai_test.go`, `intelligence_scenario_test.go`, `intelligence_insights_test.go`,
`intelligence_briefing_test.go`, `intelligence_access_test.go`, `intelligence_eval_test.go`; Playwright `mise-intelligence.spec.ts`
(8 tests: command center and AI-unavailable state with an accessibility scan, briefing idempotence, scenario projection and save,
expense approval, risk save and dismissal, meal proposal lifecycle, role scoping, phone).

Brief §39 coverage: deterministic finance, no double counting, missing expenses withhold profit, settlement vs collection, currency,
break-even, reproducible scenarios, deduplicated events, explainable segments, demo isolation (demo meals labelled and excluded from
the evidence pack; test payments apart), meal suggestions with evidence and limitations, AI cannot publish meals or execute financial
actions, restricted metrics, prompt injection, numeric reconciliation, limitations for missing data, provider failure leaves dashboards
working, idempotent scheduled briefings, budgets and request limits, existing functionality intact (regression above): all covered by
the tests named in §§3–16.

## 21. Data readiness

On the development database (27 November): **not ready**. 294 succeeded payments, all from the test provider (no real revenue);
0 of 11 expense categories covered (profit and operating results withheld); no verified demand history (forecasting inactive);
behaviour events only from test runs; inventory lots and deliveries mostly from tests; AI unavailable. The command center's readiness
panel and every view say so. This is the honest state, not a defect.

## 22. Outstanding business decisions

1. **AI provider**: which provider and model, data-processing terms (no training, retention, region), monthly budget and prices, and
   who approves (then run the real-provider evaluation and sign `MODEL_EVALUATION.md`).
2. **Risk thresholds**: replace the engineering defaults with business-approved values.
3. **Briefing schedules**: which cadences, which recipients.
4. **Expense practice**: who records and who approves; record or confirm every category monthly so profit can be shown.
5. **Cash**: record approved opening balances; decide whether supplier invoices and payments should be recorded (cash view stays
   incomplete without them).
6. **Revenue recognition policy** (recognised revenue is not calculated until one exists).
7. **Production capacity** figures for scenarios (not recorded in the platform).
8. **Meal ratings**: whether to build a rating feature (needed for rating-based meal intelligence).
9. **Forecast criteria** (from Sprint 8; still unset).

## 23. Known limitations

- Real AI is blocked; the assistant, narratives and meal concepts are verified only with a scripted model.
- Validation checks numbers and citations, not every qualitative statement; answers carry limitations and can be flagged.
- Ask Mise AI answers synchronously; streaming exists in the adapter but is not used for display (unvalidated numbers are never shown).
- Scenarios assume cash moves in the week it is earned or spent; no tax, financing or payment terms. Presets are illustrative.
- Meal proposals carry no cost estimate until a chef writes the recipe (no quantities); per-meal margin is not calculated.
- Risk and opportunity rules use engineering defaults; opportunities are observations only.
- Development data is test data; no figure here describes the real business.

## 24. Sprint 10 readiness

Sprint 9.2 adds no mobile surface, and Mise Intelligence must stay out of any customer-facing app (brief: admin only). A future
Flutter app needs nothing from this sprint. **Sprint 10 was not started** and needs explicit authorisation.

---

**Commit**: `fb4fd5e` (final Sprint 9.2 commit, 27 November 2026) on `sprint-9.2/intelligence`, after the stage checkpoints `c7c7d73`,
`83946b1`, `e13c634`, `d9d9464`, `658029f`, `76b1871`, `39fc08b`, `9da1f1c`, `b9b1ad3`. Working tree after the commit: clean
(`git status --short` empty). This note is the only change in the follow-up commit. Not pushed: pushing needs the owner's go-ahead.
