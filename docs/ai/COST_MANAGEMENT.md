# AI cost management

Sprint 9.2. How AI spending is bounded. AI ships **off with no budget**: nothing reaches a provider until an owner sets a budget and
prices and switches it on (`intelligence.configure`, Intelligence → Settings, audited).

## Settings (`intelligence_ai_settings`)

| Setting | Default | Range | Effect |
|---|---|---|---|
| enabled | off | | AI unavailable while off |
| monthly budget (US$) | none | > 0 | AI unavailable without one, and once the month's estimated cost reaches it |
| input / output price per million tokens (US$) | none | ≥ 0 | Required to switch on: without prices the budget cannot be enforced |
| price reference | — | ≤ 300 chars | Where the prices come from (provider page and date); required to switch on |
| questions per person per day | 20 | 1–1,000 | `429 ai_user_limit` at the limit (Trinidad and Tobago day) |
| tool calls per question | 6 | 1–20 | Further calls are refused and the model is told to answer with what it has |
| output tokens per reply | 1,500 | 200–8,000 | Sent to the provider as the completion limit |
| input tokens per question | 30,000 | 2,000–200,000 | The run stops when the cumulative input reaches it |
| seconds per question | 60 | 10–300 | Context deadline for the whole question |
| history retention (days) | 180 | 7–3,650 | The worker deletes older runs |
| approval reference | — | ≤ 300 chars | Who approved the provider, data processing and budget; required to switch on |

The provider, model and key are environment settings (`AI_PROVIDER`, `AI_MODEL`, `AI_API_KEY`, `AI_TIMEOUT`, `AI_RETRIES`), never stored
in the database or shown in the console. The model name is configuration, never code.

## Accounting

Each run stores the provider's reported input and output tokens and an **estimated** cost = ⌈(input × input price + output × output
price) ÷ 1,000,000⌉ in millionths of a US dollar (rounded up so it never undercounts). The month's usage (requests, tokens, estimated
cost) and the caller's day are shown in Settings and returned by `GET /api/v1/admin/intelligence/ai`. The provider's invoice is
authoritative; compare it monthly and adjust prices if they differ.

A single question can overrun the remaining budget by at most one question's cost (bounded by the token and tool-call caps); the next
question is then refused.

## Keeping requests small

- Only aggregates are sent: tools return summaries, never the database. Tables are cut to 40 rows and cells to 300 characters.
- Only the caller's tools are offered; the tool-call cap bounds round trips.
- Retries: `AI_RETRIES` (default 2) for rate limits, server errors and timeouts only; never for exhausted quota, bad requests or a
  streamed reply.
- **Briefings** are deduplicated: identical content keeps the same version, and an AI narrative is written once per version.
  Scheduled briefings are sent once per period. Meal-concept drafts are one model call each time they are requested.
- Deterministic views (command center, scenarios, risks, briefings without a narrative) never call a model.
