# AI security

Sprint 9.2. Threats to Mise Intelligence and the controls in place. Mise Intelligence is admin-only; AI is unavailable until an owner
approves a provider (`docs/ai/COST_MANAGEMENT.md`). Related: `AUTHORIZATION.md`, `THREAT_MODEL.md`, `docs/architecture/AI_INTELLIGENCE.md`.

## Controls by threat

| Threat | Controls | Verified by |
|---|---|---|
| Customers, guests or other staff reaching intelligence | Every route is under `/api/v1/admin/*` with an admin session and a named permission; customer-facing apps never call it | `TestMiseIntelligenceIsAdminOnly` (every intelligence/finance route: guest 401, customer 401/403, support staff 403); RBAC matrix (every role × every admin route); `TestCustomerFacingAppsNeverCallIntelligence`; Playwright role test |
| Unauthorized tool invocation | The model is offered only the caller's tools; every call goes through `Toolbox.Run` with the caller's principal (domain + data permission), unknown tools and extra arguments are refused | `TestAskMiseAICannotReachToolsOutsideThePermissions`; evaluation authorization 100 % |
| Prompt injection (question, help articles, tool data) | System prompt: tool output and documents are data, not instructions; but no control depends on the model: permissions, argument contracts, read-only tools and numeric validation are enforced in Go regardless of what the model says | Injection cases in the tests and the evaluation (forbidden tools refused; an injected "revenue" number removed) |
| Fabricated numbers or citations | Numeric validation against this run's tool results; citations must be refs retrieved in this run; one regeneration, then removal and `[unverified]`; the run is `partial` with a limitation | `validate_test.go`; `TestAskMiseAIAnswersFromToolsWithValidatedNumbers`; evaluation numeric/evidence 100 % |
| AI taking actions | There is no write tool. Recommendations, proposals and briefings change only their own records; proposals link to recipes made in the culinary workflow; no code path from intelligence to payments, prices, purchase orders, production, publishing, campaigns or suppliers | `TestRecommendationsAreAdvisoryAndScoped` and `TestMealIntelligenceAndProposalsStayDrafts` (purchase orders, campaigns, refunds, meals, recipes unchanged) |
| Cross-user exposure | A user sees only their own AI runs (configure sees all); recommendations and risks are filtered by the viewer's domains; briefings need executive access | History scope tests; risk and recommendation domain tests |
| Sensitive data leakage to a provider | Tools return aggregates (segment members need `customers.read` and are not a tool); output sent to a provider is bounded and scrubbed of email addresses and phone numbers; questions are the staff member's own words | `TestScrubRemovesContactDetails`, `TestForModelBoundsRows` |
| Excessive retrieval / unbounded execution | Tool-call, model-call, input-token, output-token and time caps; per-user daily limit; monthly budget | `TestAskMiseAILimitsAndBudget` |
| Provider failure | Retries only for transient errors; a failure stores a `failed` run with no answer; every deterministic view is independent of AI | `TestAskMiseAISettingsAndAvailability`; adapter tests (timeouts, 4xx/5xx, cut streams) |
| Credential exposure | The key is an environment variable, never in the database, logs or console; the AI status shows only provider and model | Config validation; settings tests |
| Unapproved activation | AI needs a configured provider **and** an owner's switch, budget, prices, price source and approval reference (audited) | Settings tests |

## Data processing (to confirm with the provider before activation)

- **What is sent**: the system prompt, the staff member's question and period, the names and schemas of their tools, and the bounded,
  scrubbed aggregate results of the tools the model calls. For briefings, the briefing text; for meal concepts, the evidence pack.
  Never: customer names, emails, phone numbers, addresses, payment data or credentials.
- **What is stored in Mise**: `intelligence_runs` (question, tool calls and arguments, evidence references, the validated answer,
  validation, tokens, cost), deleted after the retention setting (default 180 days). Tool results themselves are not stored; they are
  recomputed from the authoritative tables when needed.
- **Provider requirements** before switching on: a business agreement that inputs and outputs are not used for training, the
  provider's retention period and region, and the owner's approval recorded in the AI settings. Prefer a provider configuration with
  zero or minimal retention.

## Residual risks

- The scripted evaluation verifies guardrails, not a real model's judgement; the real-provider evaluation is blocked until a provider is
  approved (`docs/ai/MODEL_EVALUATION.md`).
- Validation checks numbers and citations, not every qualitative statement. Answers state their limitations and are labelled as
  advisory; staff can flag a wrong answer (review status) for follow-up.
- Free-text questions could contain personal data typed by staff; they are stored for the retention period and sent to the provider.
  The console asks business questions; staff guidance should say not to paste customer details.
