# API standards

Rules every Mise endpoint follows. The contract is `openapi.yaml` in this folder;
`README.md` here has the pre-launch prose reference for the lead endpoints.

## Shape

- Base path `/api/v1`. JSON only (`application/json`); unknown fields rejected; bodies
  capped (8 KB for most endpoints); one JSON object per body.
- **Success:** `{"data": …, "meta": {"request_id": "…"}}`.
- **Error:** `{"error": {"code", "message", "fields"?}, "meta": {"request_id"}}`.
  `code` is stable for programs; `message` is for people and may change; `fields` lists
  per-field problems for 422. Internal details never reach clients: unexpected failures
  are `internal_error`, logged with the request id.
- Names: `snake_case` fields, plural resource nouns (`/leads`, `/sessions`), `{id}` path
  parameters as UUIDs; malformed ids answer 404, not 400.
- Times are RFC 3339 UTC strings. Dates are `YYYY-MM-DD`. Money (later): integer minor
  units with a currency code.

## Status codes

| Status | When | Codes |
|---|---|---|
| 200 / 201 / 202 | read or change done / created / accepted for async work | |
| 400 | malformed JSON, bad token | `invalid_json`, `token_invalid` |
| 401 | no valid session for this audience, wrong credentials or code | `unauthorized`, `invalid_credentials`, `mfa_code_invalid`, `mfa_locked` |
| 403 | signed in but not allowed | `forbidden`, `mfa_required`, `email_not_verified`, `registration_closed`, `lead_unavailable` |
| 404 | missing, or not yours | `not_found`, `referral_not_found` |
| 409 | conflicts with current state | `mfa_already_enrolled`, `mfa_not_started`, `mfa_not_needed` |
| 410 | expired lead link | `token_expired` |
| 413 / 415 | body too large / not JSON | `body_too_large`, `unsupported_media_type` |
| 422 | validation | `validation_failed`, `signup_rejected` |
| 429 | rate limited (with `Retry-After`) | `rate_limited` |
| 500 / 503 | our fault / not ready | `internal_error`, `not_ready` |

## Authentication and authorisation

Bearer tokens only; the API never reads cookies and allows no credentialed CORS. Three
token kinds (lead, customer session, admin session) are never interchangeable. Each
operation's `security` in `openapi.yaml` names which it takes, and its description names
the permission. Details: `docs/security/AUTHENTICATION.md`, `AUTHORIZATION.md`.

## Pagination, filtering, sorting

- **Offset pagination** for back-office lists: `page` (from 1) and `page_size` (1 to 100,
  default 25); responses carry `items`, `total`, `page`, `page_size`.
- **Cursor pagination** for feeds and large customer-facing lists (from Sprint 2):
  `limit` and an opaque `cursor`; responses carry `items` and `next_cursor` (null at the end).
- Filters are plain query parameters named after the field (`status`, `from`, `to`);
  unknown values are 422, not ignored.
- Sorting, where offered: `sort=field` or `sort=-field`, from an allow-list per endpoint.
  Every list has a stable default order with a unique tie-breaker (`created_at, id`).

## Request ids and tracing

Every response has `X-Request-Id`. Callers may send their own (8 to 64 URL-safe
characters). A W3C `traceparent` header is read and its trace id logged beside the request id.

## Idempotency

- Natural idempotency where it exists: waitlist signup on email; verify, unsubscribe and
  reset on their token; revocation on the session id.
- **From Sprint 2**, any `POST` that creates money-bearing or order records accepts an
  `Idempotency-Key` header (a UUID). The API stores the key with the request hash and the
  first response for 24 hours, replays that response for a repeat, and answers 422 if the
  same key arrives with a different body.

## Rate limits

Per client address (the end user's, via the BFF key when called by a Next.js server):

| Bucket | Endpoints | Limit |
|---|---|---|
| Signup | `POST /leads` | 10 / hour |
| Token | lead verify and unsubscribe, account verify and reset, MFA codes | 30 / hour |
| Auth | customer and admin sign-in, register, forgot password | 20 / 15 min |
| Lookup | `GET /referrals/{code}` | 60 / min |
| Events | `POST /events` | 120 / min |
| General | everything | 600 / min |

Plus per account: sign-in 10 / 15 min per email, reset emails 3 / hour per email, 5 wrong
MFA codes per session.

## Versioning

- The version is in the path. Within `v1`, changes are additive only: new endpoints, new
  optional request fields, new response fields. Clients must ignore unknown response fields.
- Removing or changing the meaning of a field, or tightening validation, needs `v2` of
  the affected endpoints, run in parallel with `v1` until clients move (the Flutter apps
  make this matter: old app versions stay installed).
- `info.version` in `openapi.yaml` is bumped on each contract change (minor for additive).

## Contract workflow

1. Change `openapi.yaml` and the handler together.
2. `pnpm generate:api` regenerates `packages/api-client/src/schema.d.ts`.
3. Tests that keep them honest: `TestRoutesMatchOpenAPI` (Go: every served route is
   documented and vice versa), `contract-check.ts` (TypeScript: hand-written client types
   match the schema), and the CI step that fails if the generated types are stale.
