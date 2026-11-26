# Sprint 9.1 report: premium experience, multi-provider payments, Mise Chat

Branch `sprint-9.1/experience-payments-chat` (from Sprint 9 tip `3650726`). Brief: `E:\FULL MISE DEV\SPRINT 9.1`. Plan and progress:
`docs/architecture/SPRINT_9_1_PLAN.md`. Work ran 25 November 2026 (real date; the PC clock read 20 November), interrupted by one power
loss (nothing lost; local checkpoint commits). Commit hash: see §24.

## Outcome

| Area | Status |
|---|---|
| Landing page redesign (one dominant photograph) | **Verified** |
| Payment orchestration across providers | **Verified** (Go integration tests against local fake providers) |
| PayPal | **Implemented, not verified** (no sandbox credentials; PayPal cannot charge TTD) |
| Stripe | **Implemented, not verified** (no test keys; Trinidad and Tobago is not a Stripe country) |
| Scotiabank (Scotia eCom+) | **Blocked** (no public integration specification) |
| Mise Chat (widget, live delivery, inbox, notes, guests) | **Verified** (Go integration, SSE, Playwright in Chromium, Firefox, WebKit) |
| Help centre | **Verified** |
| Support analytics | **Verified** |
| Security and regression tests | **Pass** (counts in §20) |
| Launch | **Not ready**: payment provider, currency, chat staffing, help content and retention decisions are business items (§21, §22) |

Status words: **Verified** (test or measurement evidence named), **Implemented, not verified**, **Blocked** (needs someone outside
engineering), **Deferred**.

## 1. Repository audit

`SPRINT_9_1_PLAN.md` §1. In short: the hero was a collage of small hot-linked photographs; payments had one provider chosen at start-up
(`testpay` only) with no billing-ownership record and no charged/settlement/FX fields; support was an internal case list with no
customer channel, no help centre and no real-time transport; the customer and admin apps are BFFs (the browser never calls the API).

## 2. Landing page redesign

**Verified.** One self-hosted photograph dominates the hero (≈55 % desktop width, full-bleed on phones), editorial text ≈45 %, the phone
call to action on the first screen, chef section waitlist-only, stock notes on every stock photograph, final CTA restyled. Vitals
(`performance.spec.ts`, marketing only): LCP 232 → 256 ms desktop, 152 → 180 ms phone; CLS 0 both. During review the pinned
"How a Mise dinner gets made" story was found broken (content pushed to the bottom of the screen by the new stock-photo note: a grid with
three rows for four children); fixed, and `experience.spec.ts` now checks that the pinned stage, photograph and progress fit the screen.
The chat launcher was made easier to find (labelled "Chat with us" pill on tablets and desktops, ring and shadow on dark sections).

## 3. Photography and licensing

**Verified.** `docs/branding/IMAGE_SOURCES.md`: Pexels photo 27556985 (jerk chicken, rice and peas, plantain), Pexels License, downloaded
once, re-encoded to 2000 px, served through `next/image`; captioned "Serving suggestion. Stock photograph, not a Mise kit." No hot-linking.
Every other photograph is listed with source, photographer and licence. Real Mise photography remains a launch blocker.

## 4. Payment architecture

**Verified** against local fakes. `docs/architecture/PAYMENT_ARCHITECTURE.md`. The Sprint 3 payments module was extended, not duplicated:
capability-aware `Provider` adapters, a registry deciding eligibility (ready, enabled by staff, exact currency, mode matching
credentials), `GET /payment-options`, checkout with a server-validated `provider`, one attempt at a time across providers, outcomes only
from the provider (signed webhooks, the return page asking the provider, expiry reconciliation), amount/currency mismatches held for
finance, provider settings with an append-only audit and a separate live-activation permission, provider-reported fees and FX.

## 5. PayPal

**Implemented, not verified.** `docs/integrations/PAYPAL.md`. Orders v2 (create, approve, capture idempotently), captures lookup, refunds,
webhook postback verification, exact decimal conversion, fee/net/FX from the capture breakdown. Tested against a fake PayPal (9 tests:
money, readiness, bad credentials, order creation and token cache, capture-once, states incl. declined and unknown outcome, exchange
rate, refunds, webhook verification incl. outage). Found and fixed in testing: the postback re-encoded the event (would fail every
real verification) and negative amounts were mis-parsed. **TTD is not a PayPal currency**: PayPal is never offered for TTD bills; vaulting
is not assumed, so renewals would be manual (needs a business decision). Sandbox checklist in the document.

## 6. Stripe

**Implemented, not verified.** `docs/integrations/STRIPE.md`. Checkout Sessions with off-session setup, session and PaymentIntent lookup,
off-session renewal charges, refunds, expiry, webhook HMAC with tolerance and secret rotation. Tested against a fake Stripe (7 adapter
tests) and end to end in Go: chosen-provider checkout, closing an unpaid Stripe checkout before switching provider, blocking while a
payment is processing, signed webhook activation, replay, bad signature, amount mismatch held, the return page recording a payment
without a webhook, and **a renewal charged once by Mise on the card Stripe saved** (`TestStripeRenewalIsChargedOnceByMise`). External
blocker: a merchant entity in a Stripe-supported country; TTD support unconfirmed.

## 7. Scotiabank

**Blocked.** `docs/integrations/SCOTIABANK.md`. No public Scotia eCom+ integration specification (endpoints, signing, callbacks, refunds).
Registered as a provider that is never ready, offers nothing, refuses every operation and every webhook; staff cannot enable it (tested in
Go and in the browser). Needs the merchant application and Scotiabank's integration guide.

## 8. Subscription billing ownership

**Verified.** Each subscription records `billing_provider`, `billing_authority` (`mise` implemented; `provider` and `manual` modelled, not
used), `external_subscription_ref` (constraint: present exactly for provider-run billing), `renewal_status`, `consent_ref`. The scheduler
charges only `mise` + `automatic`, checked when queued and at charge time. Provider-run subscriptions (Stripe Billing, PayPal
Subscriptions) are deliberately not used, so two systems can never charge one cycle; no automatic migration between providers.

## 9. Payment security

**Verified** (local). `docs/security/PAYMENT_SECURITY.md` maps every brief §15 control to code and a named test: hosted pages only,
server amounts, idempotency keys, unique references, verified webhooks, replay protection, transactional changes, cross-provider attempt
protection, no silent cross-provider retry, refund authorisation, reconciliation, secrets from the environment only (validated at start-up,
`TestPaymentProviderConfigurationIsChecked`), masked references, live activation with approval, redacted logs, no floating-point money.

## 10. Customer chat architecture

**Verified.** `docs/architecture/SUPPORT_CHAT.md`. `internal/supportchat` in the monolith; PostgreSQL is the only store. Messages are
stored before acknowledgement, numbered per conversation, immutable, idempotent by `client_message_id`. Live delivery is Server-Sent
Events (justified: BFF apps cannot relay WebSockets; SSE passes Caddy/ALB; sending is ordinary idempotent POSTs), resumable with
`Last-Event-ID`, fanned out across API instances with PostgreSQL LISTEN/NOTIFY (no Redis), with HTTP polling fallback. Every stream
event is re-read with the caller's own authorisation; sessions are re-checked every 60 s.

## 11. Customer widget

**Verified.** `@mise/chat` on the marketing site (guests) and the customer app (customers, or guests): lazy panel, Home / Messages /
Help, full screen on phones, keyboard accessible (Playwright: open by keyboard, focus moves in, Escape returns focus), axe clean inside
the dialog, hidden on checkout pages, above the phone tab bar. Live reply in under 100 ms locally; typing both ways; Delivered/Seen;
retry for unsent messages; rating after resolution. Found and fixed in browser tests: a lingering "typing…" after a reply, the rating
thanks vanishing, a duplicate banner landmark (accessibility).

## 12. Agent inbox

**Verified.** `/support/chat`: views (waiting for us, mine, unassigned, waiting for customer, resolved, all), search, the conversation
with internal notes visibly separate, a composer with reply/note and saved replies, assign/transfer/resolve/reopen, priority, tags, links,
escalation to a support case, history, live updates, typing, read receipts. Found and fixed: the composer pushed below the fold; "Live"
shown before the connection was open; **the assignment form kept a stale "Nobody" after a live refresh** (pressing Transfer would have
unassigned the conversation). Settings page for hours, closed days, messages, email cool-down and saved replies.

## 13. CRM integration

**Verified.** Customer context (email only with `customers.read`), a link to Customer 360, links to the customer's own order and
subscription (a guest conversation cannot be linked), escalation creating a support case once and linking it, unassigned-conversation
staff alerts cleared on reply or assignment, reply emails through the existing job queue.

## 14. Help centre

**Verified.** Draft → review → published → archived, revise to draft (off the site), writers and publishers separated, plain text only,
full-text search, public `/help` and `/help/[slug]`, search in the widget. Found and fixed in browser tests: help pages were cached for
60 seconds, so a withdrawn article stayed public for up to a minute. **No article text was written** (needs approved wording).

## 15. Messaging security

**Verified.** `docs/security/MESSAGING_SECURITY.md` maps brief §32 to controls and tests: owner-scoped access (404 otherwise), note
isolation (API, stream and browser), stream authorisation, session re-check, same-origin relays (cross-origin POST → 403), hashed guest
tokens in httpOnly cookies unreadable by script, guest-to-account linking only with the token and a verified matching email, spam and
flood limits (`TestChatMessageFloodingIsLimited`), plain text only, immutable history, no message bodies in logs, no external AI.
**Open:** retention periods (proposal in §4 of that document).

## 16. Support analytics

**Verified.** 10 registry metrics in a new "Support chat" dashboard (`support_chat.analytics`): new, resolved, waiting, unassigned,
backlog, first human response time, resolution time, satisfaction responses and low/high rating counts (counts only, no score). Metric
catalogue regenerated (`docs/analytics/METRIC_CATALOG.md`).

## 17. Database migrations

| Migration | Contents |
|---|---|
| `00015_payment_providers.sql` | `payment_provider_settings`, `payment_provider_events` (append-only); billing ownership on `subscriptions`; charged/settlement/FX/fee on `payments`; `payment_providers.*` permissions. |
| `00016_support_chat.sql` | 14 chat and help-centre tables (immutable messages, notes, events), the conversation number sequence, five help categories, `support_chat.*` and `help_centre.*` permissions. |

Both are additive, have Down sections, applied on the dev database (schema version 16) and on every test run.

## 18. API contracts

`docs/api/openapi.yaml`: 52 new operations (payment options and providers; chat for customers, guests and staff; help centre) with
schemas; `TestRoutesMatchOpenAPI` passes (every route documented and vice versa). The TypeScript client was regenerated. Future
Flutter contracts: `docs/api/FLUTTER_CONTRACTS.md` (authentication without a BFF, endpoints, payloads, pagination, the stream and
reconnection, payment initiation, provider return flows, status verification, reserved push events). One small API change is noted
there for the app: accepting an app return URL per checkout.

## 19. Screenshots

`docs/screenshots/sprint-9-1/` (desktop 1440 × 900, phone 390 × 844):

| Brief §36 item | Files |
|---|---|
| New landing hero | `desktop-01-landing-hero`, `phone-01-landing-hero` |
| Updated landing page | `desktop-02-landing-page` (full page) |
| Meal catalogue | `desktop-05-meal-catalogue`, `phone-05-meal-catalogue` |
| Checkout | `*-07-checkout-address`, `*-08-checkout-review-payment` |
| Payment provider selection | `*-10-payment-provider-selection` (**staged**: placeholder Stripe *test* settings in the local API for the capture only, then removed; no Stripe call was made) |
| Payment settings | `desktop-06-payment-settings`, `desktop-06b-payment-settings-full` |
| Closed chat launcher | `*-03-chat-launcher-closed` |
| Open chat home | `*-04-chat-open-home` |
| Customer conversation | `desktop-09-customer-conversation` |
| Mobile messaging screen | `phone-09-customer-conversation-mobile-messaging` |
| Admin support inbox | `desktop-11-admin-support-inbox` |
| Customer context panel | `desktop-12-customer-context-panel` |
| Help centre | `*-13-help-centre`, `*-13b-help-article` (an article clearly marked "screenshot only", returned to draft afterwards), `desktop-15-help-article-editor` |
| Support settings | `desktop-14-support-settings` |

## 20. Automated test results

Run 25 November 2026 on the final tree.

| Suite | Command | Result |
|---|---|---|
| Go (integration on PostgreSQL) | `go test ./... -count=1 -p 1` | 19 packages ok; **199** test functions (170 at the end of Sprint 9; 29 new) |
| Go static | `go vet ./...`; `gofmt -l`; `govulncheck ./...` | clean; clean; 0 reachable (1 unreachable, as in Sprint 9) |
| TypeScript unit | `pnpm test` | Vitest **45** passed (admin 7, web 15, marketing 23) |
| Typecheck / lint | `pnpm -r typecheck`; `pnpm -r lint` | clean in every workspace (incl. `@mise/chat`, e2e) |
| API contract | `TestRoutesMatchOpenAPI` | pass |
| RBAC matrix | `TestEveryRoleReachesExactlyTheAdminEndpointsItIsGranted` | 26 roles × 386 admin routes = 9,264 calls, pass |
| Dependencies | `pnpm audit --prod` | no known vulnerabilities |
| Playwright, Chromium, launch mode | 16 spec files in 3 batches, `--workers=1` | **64 passed, 0 failed, 5 skipped** |
| Playwright, Chromium, waitlist mode | `waitlist experience launch` (marketing built with `SITE_MODE=waitlist`) | **25 passed, 0 failed** |
| Playwright, Firefox | `journey launch responsive accounts customer-platform subscription-commerce chat payment-providers` | **40 passed** |
| Playwright, WebKit desktop | same | **38 passed, 2 skipped** |
| Playwright, WebKit iPhone 13 size | same | **36 passed, 2 failed, 2 skipped** |
| Browser walkthrough | `e2e/tools/chat-walkthrough.mjs` | all checks pass |

**Skipped and failed, explained.** Chromium skipped 5: `performance.spec.ts` and `screenshots.spec.ts` run only with `E2E_PERF=1` /
`E2E_SHOTS=1` (Sprint 9 measurements; Sprint 9.1 hero vitals in §2). WebKit skipped 2: the two Tab-to-link keyboard tests (WebKit does
not Tab to links by default; needs a manual Safari check). The 2 iPhone-size WebKit failures are the same two as in Sprint 9: older tests
that click desktop-only controls (the desktop "Main" navigation and the desktop "Your box" sidebar); the phone versions of those flows
pass. Test scope, not app defects. The new chat button caused no failure in any engine.

**First runs failed, then fixed.** Chromium: the Sprint 3 commerce spec looked for the old Payments wording (updated), and chat feedback
(real bug, §11). The new specs also surfaced the help cache, the live-indicator and stale-form bugs (§12, §14) and several test
mistakes (selector scope, waiting for the right connection). How the counts were assembled: the three launch-mode batches ran once;
the tests touched by later fixes were then re-run on the rebuilt apps (the Sprint 3 commerce test after its update; `chat.spec.ts`
after each chat fix, last 4/4 in launch mode and 4/4 in waitlist mode, after the final admin fix). The admin and chat fixes made after the
cross-browser runs touched only `@mise/chat` and the admin inbox; the cross-browser runs were not repeated after them.

**Discovered, executed, passed, failed, skipped** (Playwright, all engines): 18 spec files discovered; 16 executed in launch mode and 3
in waitlist mode (launch.spec in both); every non-opt-in test executed. Totals across the five runs: 203 passed, 2 failed (iPhone-size
test scope), 9 skipped (5 opt-in, 4 WebKit keyboard).

**Not run:** real PayPal and Stripe sandboxes (no credentials), real Safari and real devices, a screen reader, `gitleaks` (Sprint 9's
scan stands; no secrets were added: provider settings are empty in `.env.example`). Production load and performance were not re-measured.

## 21. Outstanding business dependencies

1. A merchant account (PayPal, Stripe and/or Scotiabank) and sandbox credentials; Stripe needs an entity in a supported country.
2. Currency policy: bills are TTD; PayPal cannot charge TTD; Stripe TTD unconfirmed; Mise never converts.
3. Renewal consent wording (`consent_ref`) and whether a manual weekly payment (PayPal) may be offered.
4. Card routing if two card processors are enabled.
5. Scotiabank's merchant approval and integration guide.
6. Chat staffing: agents, business hours, closed days, response expectation, saved replies (all ship empty).
7. Help articles with approved wording (none written).
8. Chat data retention and deletion periods; a customer account deletion feature does not exist yet (platform-wide).
9. Attachments: approved private storage and malware scanning (deferred).
10. Everything still open from Sprint 9 (domain, TLS, AWS, email provider, prices, menus, legal review…).

## 22. Launch readiness

**Not ready.** `docs/launch/GO_LIVE_CHECKLIST.md` updated: #10 (payment provider) and #11 (webhook verification) rewritten; new 13a
(currency and card routing), 13b (renewal consent), 23a (chat staffing), 23b (help articles), 23c (chat retention), 23d (attachments,
deferred). The Launch page's `payment_provider` gate now asks the registry live and reports "Only test-mode providers are taking
payments (Test payments (simulator))". Nothing here enables live payments or a public launch.

## 23. Sprint 10 readiness

The messaging and payment APIs are ready for a native client: `docs/api/FLUTTER_CONTRACTS.md` covers authentication, chat endpoints and
the stream, pagination, reconnection, payment options, initiation and status verification. To add before or in Sprint 10: an app return
URL per checkout (small API change), a device registry and push sender for the reserved events, and the retention purge job once periods
are decided. Sprint 10 needs explicit authorisation; it was not started.

**Pre-existing, noted:** unknown catalogue and help pages answer 200 with a `noindex` robots tag (streamed not-found) alongside the
store layout's `index, follow` tag; search engines apply the stricter one, but the duplicate should be tidied.

## 24. Commit

Committed on `sprint-9.1/experience-payments-chat` as `0497ddd`. Not pushed (local checkpoints were
`f2ead1d`, `3e39999`, `4023b4d`, `7e53ed9`, `953b0ff`, `1776733`).
