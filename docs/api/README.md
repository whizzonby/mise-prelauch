# Mise API

REST, JSON, versioned under `/api/v1`. The API is the system of record: the web apps, and
later the Flutter app, hold no business rules of their own.

## Conventions

**Success**

```json
{ "data": { }, "meta": { "request_id": "7ad1748e2086997364d46935" } }
```

**Error**

```json
{
  "error": {
    "code": "validation_failed",
    "message": "Some fields need attention.",
    "fields": [{ "field": "email", "code": "invalid", "message": "Enter a valid email address, like name@example.com." }]
  },
  "meta": { "request_id": "…" }
}
```

- `error.code` is stable and meant for programs; `error.message` is written for people and may change.
- Every response carries `X-Request-Id`. Send your own (8 to 64 URL-safe characters) to trace a request across systems.
- Request bodies must be `application/json`. Unknown fields are rejected.
- Errors never include stack traces or internal detail. Unexpected failures return `internal_error` and are logged with the request id.
- Timestamps are RFC 3339 in UTC.

| Status | Codes |
|---|---|
| 400 | `invalid_json`, `token_invalid` |
| 401 | `unauthorized`, `invalid_credentials` |
| 403 | `forbidden`, `lead_unavailable` |
| 404 | `not_found`, `referral_not_found` |
| 410 | `token_expired` |
| 413 | `body_too_large` |
| 415 | `unsupported_media_type` |
| 422 | `validation_failed`, `signup_rejected` |
| 429 | `rate_limited` (with `Retry-After`) |
| 500 | `internal_error` |
| 503 | `not_ready` |

**Rate limits** are per client address: signup 10 an hour, token endpoints 30 an hour,
referral lookup 60 a minute, events 120 a minute, admin sign-in 20 per 15 minutes (plus 10
per 15 minutes per account), everything 600 a minute.

**Idempotency.** Signup is idempotent on the email address: repeating it never creates a
second lead. Verification and unsubscribe are idempotent on their token.

**CORS.** Only the origins in `CORS_ORIGINS` may call the API from a browser. Credentials
(cookies) are never allowed; browsers authenticate with bearer tokens.

## Health

| | |
|---|---|
| `GET /api/v1/health` | Liveness: the process is serving. |
| `GET /api/v1/ready` | Readiness: the database is reachable. 503 `not_ready` otherwise. |

## Leads

### `POST /api/v1/leads`

Join the waitlist.

```json
{
  "first_name": "Asha",
  "email": "asha@example.com",
  "phone": "+1 868 555 0100",
  "location": "port-of-spain",
  "dietary_interests": ["vegetarian"],
  "household_size": 3,
  "consent": true,
  "referral_code": "ABC2345",
  "attribution": {
    "first": { "utm_source": "instagram", "utm_medium": "social", "utm_campaign": "launch", "landing_page": "/", "referrer_url": "https://instagram.com/" },
    "latest": { "utm_source": "newsletter", "landing_page": "/" }
  },
  "anonymous_id": "c2b7…",
  "website": "",
  "elapsed_ms": 9400
}
```

- Required: `first_name`, `email`, `location`, `consent: true`.
- `location`, `dietary_interests` are option values (lower-case letters, digits, hyphens).
- `website` is a honeypot and must be empty; `elapsed_ms` is how long the form was open. A filled honeypot or a form submitted in under 1.5 seconds returns 422 `signup_rejected`.
- An unknown `referral_code` is ignored, not an error.

`201 Created` for a new lead:

```json
{
  "data": {
    "outcome": "created",
    "lead": {
      "first_name": "Asha",
      "status": "PENDING",
      "verification_required": true,
      "referral_code": "3TPDVC9",
      "referral_url": "https://mise.tt/join/3TPDVC9",
      "referrals": { "pending": 0, "converted": 0 },
      "profile_completed": false
    },
    "profile_token": "…"
  }
}
```

`200 OK` when the address is already on the list. No lead data is returned; the existing
lead is emailed their link instead (at most once every ten minutes):

```json
{ "data": { "outcome": "already_on_list" } }
```

Two addresses are the same lead if they match after lower-casing and removing a `+tag`
(and, for Gmail, dots).

### `POST /api/v1/leads/verify`

`{ "token": "…" }` from the verification email. Returns `{ lead, profile_token }`. Marks
the lead `VERIFIED`, counts the referral for whoever invited them, and queues the welcome
email. Tokens last 7 days.

### `GET /api/v1/leads/me`

`Authorization: Bearer <profile_token>`. Returns the lead summary shown above, with
current referral counts. Profile tokens last 30 days.

### `PATCH /api/v1/leads/preferences`

`Authorization: Bearer <profile_token>`. Progressive profiling. Every field is optional;
fields that are sent replace the stored value, fields that are left out are kept.

```json
{
  "household_size": 4,
  "meals_per_week": 3,
  "dietary_preferences": ["vegetarian"],
  "meal_interests": ["caribbean-classics", "family"],
  "cooking_frequency": "most-days",
  "delivery_area": "port-of-spain",
  "household_type": "family",
  "fitness_goal": "build-muscle",
  "usage": "weeknight-dinners"
}
```

### `POST /api/v1/leads/unsubscribe`

`{ "token": "…" }` from the unsubscribe link in any email. Sets the lead to `UNSUBSCRIBED`.
Unsubscribe tokens do not expire.

## Referrals

### `GET /api/v1/referrals/{code}`

```json
{ "data": { "code": "3TPDVC9", "referrer_first_name": "Asha" } }
```

404 `referral_not_found` if the code is unknown or its owner is blocked.

A referral is `pending` until the invited lead verifies their email, then `converted`.
It is `flagged`, and never counted, when the invited lead signs up from the same network
address as the referrer.

## Events

### `POST /api/v1/events`

```json
{
  "anonymous_id": "c2b7…",
  "session_id": "9f31…",
  "events": [
    { "type": "page_view", "metadata": { "path": "/" } },
    { "type": "faq_opened", "metadata": { "question": "pricing" } }
  ]
}
```

1 to 20 events. `type` must be in the taxonomy (`docs/architecture/analytics.md`).
`metadata` is a flat object of at most 8 keys whose values are strings (up to 200
characters), numbers or booleans. Sending a profile token attaches the events to the lead.
Returns `202 { "accepted": n }`.

## Admin

All admin endpoints need `Authorization: Bearer <session token>` and check a permission.

| Endpoint | Permission | |
|---|---|---|
| `POST /api/v1/admin/auth/login` | none | `{ email, password }` → `{ token, expires_in, admin }` |
| `POST /api/v1/admin/auth/logout` | signed in | Ends the session. |
| `GET /api/v1/admin/auth/me` | signed in | The signed-in admin, role and permissions. |
| `GET /api/v1/admin/stats/overview?days=30` | `stats:read` | Totals, signups by day, sources, dietary, household and location breakdowns. `days` 7 to 180. |
| `GET /api/v1/admin/leads` | `leads:read` | Paginated list. Filters: `q`, `status`, `location`, `source`, `referral` (`referrer` or `referred`), `from`, `to` (`YYYY-MM-DD`), `page`, `page_size` (max 100). |
| `GET /api/v1/admin/leads/{id}` | `leads:read` | Profile, preferences, attribution, referral history, activity timeline. |
| `PATCH /api/v1/admin/leads/{id}` | `leads:write` | `{ "status": "QUALIFIED" }`. Allowed: `VERIFIED`, `QUALIFIED`, `UNSUBSCRIBED`, `BLOCKED`. Audited. |
| `DELETE /api/v1/admin/leads/{id}` | `leads:erase` | Erasure: deletes the lead and everything attached. Audited. |
| `GET /api/v1/admin/leads/export.csv` | `leads:export` | CSV of the filtered list (same filters), up to 50,000 rows. The audit entry is written before any row is sent. |

Session tokens are opaque, expire after `ADMIN_SESSION_TTL` (12 hours by default) and are
stored only as SHA-256 hashes.
