# Sprint 9.1 plan: premium experience, multi-provider payments, Mise Chat

Brief: `E:\FULL MISE DEV\SPRINT 9.1`. Branch `sprint-9.1/experience-payments-chat` (from Sprint 9 tip `3650726`). Authorised
25 November 2026. Hard limits: no public launch, no live payments, do not start Sprint 10.

Owner decisions (25 November 2026):
- **No payment sandbox credentials.** PayPal and Stripe adapters are built to the official documentation and tested against local fake
  provider servers that follow the documented contracts. Real sandbox verification is **Blocked** until credentials exist.
- **Hero photograph:** one Pexels photograph chosen by engineering, self-hosted and optimised, licence recorded.
- **Attachments disabled:** no approved private object storage; built behind a storage interface, switched off, blocker documented.

## 1. Audit (what exists)

| Area | Finding |
|---|---|
| Hero | `apps/marketing/src/components/hero.tsx`: headline lines plus a "counter" grid of several small ingredient photographs (prelaunch composition the brief asks to replace). Photographs are hot-linked Pexels images listed in `content/image-credits.ts` and `/credits`. |
| Other prelaunch imagery | Supply story, chef feature and ingredient story sections use several small Pexels photos; waitlist copy remains for `SITE_MODE=waitlist` (kept: it is the pre-launch mode). |
| Payments | `internal/payments`: provider-agnostic `Provider` interface with `Capabilities`, checkout sessions, webhooks stored once per event, forward-only statuses, reconciliation, refunds, `CheckoutCanceller` (Sprint 9). **One** checkout provider (`Service.Checkout`), chosen at start-up. Mise owns renewals (off-session `Charge` on a saved `payment_methods` token). Only `testpay` exists. |
| Money | Integer minor units everywhere; orders and payments carry one currency; no charged/settlement/FX/fee fields. |
| Subscriptions | `payment_method_id` only: no record of billing provider or billing authority. |
| Support | `internal/support`: `support_cases` + append-only `support_case_events` (notes, status, assignment); `cases.read`/`cases.manage`; Customer 360 in the admin. No chat, no help centre, no guest contact path other than email. |
| Real time | None. The customer app is a BFF: the browser never calls the API; sessions live in httpOnly cookies on the app host. No WebSocket library; no Redis deployed. |
| Analytics | Sprint 8 registry (`internal/analytics/metrics.go`), 52 metrics, per-domain permissions. |

## 2. Verified provider facts (official sources, read 25 Nov 2026)

| | PayPal | Stripe | Scotiabank (Scotia eCom+) |
|---|---|---|---|
| TTD | **Not supported** (not among PayPal's 24 currency codes) | **Unconfirmed** (presentment table not readable); adapter only offers currencies the business confirms | Not stated publicly |
| Merchant country | Not checked for TT business accounts | **Trinidad and Tobago is not a Stripe-supported country** (stripe.com/global) | TT bank product; onboarding by email to merchant services |
| Hosted flow | Orders v2: `POST /v2/checkout/orders`, buyer approval link, `POST /v2/checkout/orders/{id}/capture` | Checkout Session `mode=payment`, `payment_intent_data[setup_future_usage]=off_session` | "Hosted Payments" (redirect to bank page); **no public specification** |
| Webhooks | Postback `POST /v1/notifications/verify-webhook-signature` → `verification_status: SUCCESS` | `Stripe-Signature: t=…,v1=…`; HMAC-SHA256 over `t + "." + body` with the endpoint secret; 5-minute tolerance | Not public |
| Idempotency | `PayPal-Request-Id` | `Idempotency-Key` | Not public |
| Later charges | Vaulting needs separate PayPal approval (not assumed) | Off-session PaymentIntent (`off_session=true, confirm=true`) | Not stated |
| Refunds | Captures refund API | `POST /v1/refunds` | Terminal refunds only (FAQ); online refunds not stated |

Consequences: **PayPal can only charge a non-TTD currency**, which requires an approved conversion and disclosure policy (none exists:
PayPal is therefore not offered for TTD orders). **Stripe needs a merchant entity in a supported country** (external blocker).
**Scotiabank processing is Blocked**: only registration, configuration and capability boundaries are built.

## 3. Design (Part A)

- One dominant photograph (≈55 % desktop width, full-bleed on phone with the focal point preserved), editorial text ≈45 %,
  existing tokens and typography, two working CTAs (catalogue, plans; waitlist mode keeps "Join the waitlist").
- Self-hosted image in `apps/marketing/public/images/hero/`, served through `next/image` (AVIF/WebP, responsive `sizes`, `priority`,
  explicit dimensions to keep CLS at 0). Measured LCP/CLS before and after with `performance.spec.ts`.
- Remove the small-photo counter grid and other collage/mosaic compositions from the landing page; keep working functional sections.
- `docs/branding/IMAGE_SOURCES.md`: every photograph, source URL, photographer, licence, and "stock, not Mise" statement.

## 4. Payments (Part B)

Extend `internal/payments` (no second commerce system):
- **Registry and capabilities**: each provider declares `Currencies`, `OffSessionCharges`, `Refunds`, `HostedCheckout`, `Live`,
  `Status` (`enabled`/`configured`/`blocked` with reason). `GET /api/v1/payment-options?currency=TTD&amount=…` returns only providers
  that can take that exact currency now. Checkout takes `provider` and validates it server-side.
- **Provider settings** (migration): `payment_provider_settings` (enabled, mode test/live, merchant verification status, approved_by,
  approved_at) with an append-only `payment_provider_events` audit trail. Secrets stay in environment/Secrets Manager; the API
  shows masked references only. Live activation needs `payment_providers.activate` **and** production configuration; refused while
  the provider adapter reports test credentials.
- **Billing ownership** on `subscriptions`: `billing_provider`, `billing_authority` (`mise` | `provider` | `manual`),
  `external_subscription_ref`, `payment_method_ref`, `renewal_status`, `consent_ref`. Mise-managed authorised billing is the only
  model implemented (Stripe off-session, testpay). PayPal Subscriptions (provider-managed) is **not** implemented: it would make two
  systems able to charge one cycle. No automatic migration between providers.
- **Money fields** on `payments`/`refunds`: `charged_currency/amount`, `settlement_currency/amount`, `fx_rate` (numeric), `fx_source`,
  `fx_at`, `fee_minor/currency` — filled only from provider data; no conversion performed.
- **Adapters**: `paypal` (OAuth client credentials with token cache, create order with `PayPal-Request-Id`, approval redirect,
  capture on return/webhook, order lookup for verification, refund, webhook postback verification), `stripe` (Checkout Session,
  webhook HMAC verification, session/PaymentIntent lookup, off-session charge, refund), `scotiabank` (registration and settings only;
  every operation returns a Blocked error). Tests run each adapter against an `httptest` server that implements the documented
  requests and responses, including bad signatures, replays, amount/currency mismatch, failures and cancellations.
- **Safety**: one open checkout per bill across providers (lock the bill); a payment in an uncertain state blocks a new attempt with a
  different provider until reconciled; amount and currency from the provider are compared with the bill and a mismatch is flagged,
  never applied; no automatic cross-provider retry.
- **Admin**: `/payments/providers` (status, mode, currencies, recurring capability, last reconciliation, failures, webhook health,
  masked credential references, audited enable/approve actions).

## 5. Mise Chat (Part C)

- **Module** `internal/supportchat`; tables (additive migration): `support_conversations` (customer or guest, status, priority, tags,
  assignee, team, `case_id` link to `support_cases`, order/subscription links), `support_messages` (author kind customer/agent/system,
  `client_message_id` unique per conversation, `seq` per conversation), `support_read_positions`, `support_conversation_events`
  (append-only: assign, transfer, resolve, reopen, escalate, link), `support_internal_notes`, `support_saved_replies`,
  `support_feedback`, `support_business_hours` + `support_holidays` + settings, `support_guest_sessions` (hashed opaque tokens),
  `help_articles` + `help_article_events` (draft → review → published → archived). Attachments table **not created** (disabled).
- **Transport**: HTTP for history and sending (idempotent by `client_message_id`; persisted before acknowledgement); **Server-Sent
  Events** for live events (`/api/v1/me/chat/stream`, `/api/v1/chat/guest/stream`, `/api/v1/admin/chat/stream`) with `Last-Event-ID`
  / `after=seq` catch-up. Fan-out across API instances via PostgreSQL `LISTEN/NOTIFY` (no Redis). The Next apps relay the stream
  (BFF), so session cookies never leave the app host. Authorisation re-checked on every event delivery and the session revalidated
  periodically on long-lived streams. Justification: the BFF architecture cannot proxy WebSockets through Next route handlers; SSE
  works through it and through Caddy/ALB, and HTTP fallback is the same endpoints.
- **Typing and read receipts**: typing is ephemeral (NOTIFY only, throttled); read receipts from persisted read positions only.
  "Delivered" means persisted by the server, nothing more.
- **Guests**: create with name + email (no verification needed to chat), rate-limited per address, opaque token in an httpOnly cookie.
  Guest conversations never show account data. Linking to an account only through a signed-in, email-verified customer whose
  verified email matches **and** who holds the guest token (same browser), never by email match alone.
- **Agent inbox** (admin `/support/chat`): three columns (list with views and search, conversation with composer, customer context
  from Customer 360 limited by the viewer's permissions), assign/transfer/notes/saved replies/tags/priority/resolve/reopen/escalate to
  a support case/link order or subscription. Internal notes are a separate table and API; never in customer payloads.
- **Help centre**: public `/help` (customer app) with search and categories, admin editor with the review workflow; articles appear
  in the widget. Only published articles are public; no policy or nutrition text is invented (seed only structural categories).
- **Offline**: business hours, timezone, holidays, availability message and response-expectation text are admin-configured and empty
  by default (shown as "We'll reply as soon as we can", never a promised time).
- **Notifications**: jobs with dedupe keys and per-conversation cool-down (one email per conversation per window, not per message).
- **Analytics**: registry metrics under a new `support` domain: new, open, resolved conversations; first human response time;
  resolution time; unassigned; backlog; satisfaction responses (count and distribution only, never an invented score).
- **Permissions**: `support_chat.read/.reply/.assign/.transfer/.resolve/.internal_notes/.manage/.analytics`, `help_centre.edit/.publish`,
  `payment_providers.read/.manage/.activate`, granted in the migration (support, operations, admin, super_admin, finance as fits).
- **Widget**: shared package `@mise/chat` used by the marketing site and the customer app; lazy-loaded after first paint; launcher in
  Mise green; Home / Messages / Help; full-screen on phones; keyboard accessible; positioned clear of checkout actions and the
  customer app's bottom tab bar; hidden on checkout and payment pages.

## 6. Testing

Go integration tests (PostgreSQL) for payments (each adapter against fake servers, options eligibility, disabled provider, bad
credentials, success/failure/cancel, duplicate and invalid webhooks, amount/currency mismatch, concurrent attempts, uncertain outcome,
refund, duplicate renewal prevention, reconciliation, analytics events) and chat (guest/auth create, both directions, SSE delivery,
persistence, catch-up after reconnect, duplicate `client_message_id`, assignment, note isolation, unread counts, resolve/reopen,
cross-customer isolation, unauthorised stream subscription, guest ownership, offline hours, notification dedupe). Playwright:
hero (LCP/CLS, widths, axe), checkout provider selection, widget (guest and signed-in, real-time agent reply, keyboard, phone),
inbox, help centre; full Sprint 1–9 regression with counts; cross-browser for the new customer specs.

## Progress

Resume from the first unchecked item.

- [x] Stage 1 — Audit, provider research, this plan
- [x] Stage 2 — Landing hero and image sources — 25 Nov: one Pexels photo (27556985, self-hosted 2000px), 45/55 layout, phone CTA on first screen; chef section waitlist-only; stock notes; final CTA restyle; IMAGE_SOURCES.md; vitals before 232/152 ms vs after 256/180 ms LCP (desktop/phone, marketing only), CLS 0 both; launch.spec 9/9
- [ ] Stage 3 — Payments: migration, registry/options, settings + audit, billing ownership, money fields
- [ ] Stage 4 — Payments: PayPal, Stripe, Scotiabank adapters + fake-server tests; checkout UI; admin providers page
- [ ] Stage 5 — Chat backend: migration, conversations/messages/guests, SSE + LISTEN/NOTIFY, agent actions, notifications, help centre, analytics, permissions
- [ ] Stage 6 — Chat UI: widget package, BFF relays in both apps, admin inbox + context + settings, help centre pages
- [ ] Stage 7 — Docs (payment, integrations, chat, security, image sources, Flutter contracts, go-live update), OpenAPI + client
- [ ] Stage 8 — Verification (tests, screenshots, regression counts, cross-browser), report, commit
