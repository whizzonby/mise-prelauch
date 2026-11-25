# Flutter integration contracts: chat, help and payments

For the future Mise mobile apps (Sprint 10+; nothing mobile is built yet). Everything below is already served by the API and covered
by `docs/api/openapi.yaml` (generate a Dart client from it, for example with `openapi-generator`'s `dart-dio`). Conventions
(envelopes, errors, ids, money, times) are in [API_STANDARDS.md](API_STANDARDS.md).

## 1. Authentication

The web apps are BFFs; a native app is not. It calls the API directly (`https://<api host>/api/v1/...`):

- **Customer.** `POST /auth/login` returns a session token. Keep it in the platform keystore (Keychain / Android Keystore via
  `flutter_secure_storage`), send it as `Authorization: Bearer <token>`, and treat any `401` as signed out. Sessions expire after
  inactivity (`CUSTOMER_SESSION_IDLE_TTL`); there is no refresh token: sign in again.
- **Guest chat.** `POST /chat/guest/conversations` returns `data.guest.token` once. Keep it in secure storage and send it as
  `X-Guest-Token: <token>`. It expires after 30 days without use (`401 guest_session_invalid` → forget it).
- **Joining guest history to an account.** After sign-in, `POST /me/chat/link-guest` with both `Authorization` and `X-Guest-Token`.
  Allowed only if the account's email is verified and equals the guest's. On success (or `401`), delete the stored guest token.
- Never log tokens or message text. Certificate pinning is optional; TLS is required.

## 2. Chat endpoints

Owner endpoints exist twice with identical shapes: `/me/chat/...` (customer) and `/chat/guest/...` (guest).

| Method and path | Purpose | Notes |
|---|---|---|
| `GET /chat/availability` | `{ enabled, online (bool or null), message, response_expectation }` | Public. `online: null` means no hours are configured: show the expectation text only. |
| `POST /me/chat/conversations` | Start (customer): `{ body, client_message_id, subject?, source: "app" }` → `201 { conversation, message }` | Retrying with the same `client_message_id` returns the same conversation. |
| `POST /chat/guest/conversations` | Start (guest): `{ name, email, body, client_message_id, source }` → `201 { guest{ id, name, email, token }, conversation, message }` | `429 rate_limited` (5 starts per email per hour), `503 chat_unavailable` when chat is off. |
| `GET …/conversations` | The caller's conversations, newest first (up to 50) | Each has `unread` (team messages after the caller's read position) and `team_read_seq`. |
| `GET …/conversations/{id}` | One conversation | `404` for anything not the caller's. |
| `GET …/conversations/{id}/messages?after=<seq>` | Messages with `seq > after`, oldest first, at most 200 | **Pagination:** call again with the last seq received until fewer than 200 come back. |
| `POST …/conversations/{id}/messages` | `{ body (1–4000 chars), client_message_id }` → `201 Message` | Stored before the answer. Same id → same message. `429 rate_limited` above 20 a minute. A resolved conversation reopens. |
| `POST …/conversations/{id}/read` | `{ seq }` | Call when messages are on screen; it drives the team's "Seen". |
| `POST …/conversations/{id}/typing` | — | At most every 3 s while typing; never stored. |
| `POST …/conversations/{id}/feedback` | `{ rating: 1–5, comment? }` | Offer once the conversation is resolved. |
| `GET …/conversations/{id}/stream` | Server-Sent Events (§3) | |
| `GET /help/categories`, `GET /help/articles?q=&category=`, `GET /help/articles/{slug}` | Help centre (published only) | Bodies are plain text: split on blank lines into paragraphs; never render as HTML. |

**Message** = `{ conversation_id, seq, author: customer|guest|agent|system, author_name, body, client_message_id, created_at }`.
Show agent messages with `author_name` (an agent's first name or "Mise team"). **Conversation** = `{ id, number, status:
open|pending|resolved, subject, last_seq, last_message, last_message_at, unread, team_read_seq, feedback_rating, created_at, updated_at }`.

`client_message_id`: 8–64 characters of `A–Z a–z 0–9 _ -` (a UUID v4 is fine). Generate it **before** the first send attempt and reuse it
for every retry of that message.

## 3. Real-time contract

`GET …/conversations/{id}/stream` with the same authentication headers, `Accept: text/event-stream`. Resume with the
`Last-Event-ID: <seq>` header (or `?after=<seq>`). Events:

| Event | `id` | `data` | Client action |
|---|---|---|---|
| `message` | the seq | a Message | Insert or replace (match by `seq`, or by `client_message_id` for your own pending message). |
| `conversation` | — | a Conversation | Update status and the "Seen" marker (`team_read_seq`). |
| `typing` | — | `{ "who": "agent" }` | Show "typing…" for about 4 s, and clear it when an agent message arrives. |
| `end` | — | `{ "reason": "session_ended" }` | Stop; the session is no longer valid (sign in again). |
| comment `: ping` | — | — | Heartbeat every 15 s. |

**Reconnection.** The server closes a stream after about 25 minutes, and networks drop. Reconnect with backoff (1 s, 2 s, 5 s, then
every 15 s), sending the last seq you hold; the server replays everything after it, so nothing is lost and duplicates are recognised by
seq. On `401`/`404` do not reconnect. While the app is in the background, close the stream; on resume, fetch `messages?after=` and
reopen it. **Fallback:** if streaming is not possible, poll `GET …/messages?after=` and `GET …/conversations/{id}` every 15 s.
Sending never depends on the stream: it is always an ordinary POST.

Dart: `http` with a streamed request and a small SSE line parser, or a package such as `flutter_client_sse`, wrapped to add the
backoff and `Last-Event-ID` handling above.

## 4. Payments

**Options.** `GET /payment-options?currency=TTD` → `{ currency, items: [{ provider, label, method: card|paypal, test, recurring }] }`.
Show only these. `test: true` means no real money moves (say so). `recurring: false` means the method cannot be charged automatically
for later weeks (show "You'll pay each week yourself").

**Initiation.**

| Step | Call | Result |
|---|---|---|
| Price | `POST /me/subscriptions/quote { plan_id, promotion_code? }` | The exact amount and currency to show before payment (`due_now`, `renewal`). |
| Subscribe | `POST /me/subscriptions { plan_id, address_id, promotion_code?, provider? }` | `201 { subscription_id, checkout: { id, redirect_url, … } }` (or `activated: true` when nothing is due). |
| Pay a week | `POST /checkout/sessions { billing_cycle_id, provider? }` | `201 Checkout`. `409 payment_in_progress` / `payment_status_unknown`: an earlier attempt is unresolved; wait and retry. `422 payment_method_unavailable`: refresh the options. |

**Provider return flow.** Open `redirect_url` in an in-app browser session (`flutter_web_auth_2`: ASWebAuthenticationSession on
iOS, Custom Tabs on Android), never in a WebView that could read card fields. The providers return to the web result page
(`/subscribe/result`) today. For the app, the API will need to accept a registered app return URL (custom scheme or universal/app
link) per checkout; **that is a small API change still to make** (`ReturnURL` is server-set). Until then, close the browser session when
the user returns and go to the status check.

**Status verification.** Never trust the return. Poll `GET /checkout/sessions/{id}` every 2 s for up to 2 minutes:
`outcome: pending | succeeded | failed | expired`, plus `subscription_status` and `invoice_id`. While the customer waits, the API asks
the provider itself, so PayPal approvals are captured without waiting for a webhook. On `succeeded` show the confirmation; on `failed`
or `expired` offer "try again" (a new checkout for the same bill). If still pending after 2 minutes, say the payment is being
confirmed and show the subscription page (webhooks and reconciliation will settle it).

Card data never touches the app. No provider SDK is needed for hosted checkout; adding Stripe's native PaymentSheet later would be a
separate, reviewed change.

## 5. Future push notification events

Not built (no push provider or device registry yet). Reserved names and payloads (ids only; the app fetches content after opening):

| Event | Payload | When |
|---|---|---|
| `chat.reply` | `{ conversation_id }` | An agent replied and the customer has not read it (same rule and cool-down as the reply email). |
| `chat.resolved` | `{ conversation_id }` | Optional; for a feedback prompt. |
| `payment.succeeded` | `{ checkout_id?, billing_cycle_id }` | A week was paid. |
| `payment.failed` | `{ billing_cycle_id }` | A renewal failed; the customer must pay the week. |
| `renewal.requires_action` | `{ billing_cycle_id }` | The saved card needs authentication; pay through checkout. |
| `delivery.*` | `{ order_id }` | Mirrors the Sprint 7 delivery emails. |

Needed first: `POST /me/devices { platform, push_token }` (and delete), consent for push in the profile, and a sender job (FCM/APNs)
reading the same outbox events the emails use.
