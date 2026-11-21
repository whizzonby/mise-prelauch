# Sprint 10 plan: the Mise customer app (Flutter, iOS and Android)

Brief: `E:\FULL MISE DEV\Sprint 10`. Audit: [SPRINT_10_AUDIT.md](SPRINT_10_AUDIT.md). Branch `sprint-10/mobile-app` off `fb04028`.
Resume from **Progress** at the bottom after any interruption.

## 1. Decisions

| Topic | Decision | Why |
|---|---|---|
| Location | `apps/mobile` (Flutter project, not a pnpm package) | Fits the monorepo; pnpm ignores it. |
| Toolchain | Flutter 3.38.7 / Dart 3.10.7 stable (installed) | Current stable on this machine. |
| State | `flutter_riverpod` (no code generation) | Brief §5; providers stay explicit. |
| Navigation | `go_router` with `StatefulShellRoute` (5 tabs, state kept per tab) | Brief §10; deep links and back behaviour. |
| Networking | `dio` with an auth interceptor (attach access token, refresh once on `401`, single-flight) | Brief §5. |
| Models | Hand-written immutable Dart classes with `fromJson`, no Freezed/json_serializable/Drift | The brief allows "where beneficial". `build_runner` is one of the heaviest jobs on this PC, and the model set is moderate. A contract test checks every path the Dart client calls exists in `docs/api/openapi.yaml`. |
| Local cache | JSON files in app-private storage (`path_provider`), excluded from backups, labelled with fetch time, wiped at sign-out | Offline reads only (§42). Drift is deferred until a real query need appears. |
| Secrets | `flutter_secure_storage` (Keychain / Android Keystore-backed) for tokens; `shared_preferences` only for non-personal flags (onboarding seen) | §13. |
| Payments | Hosted checkout in a system browser session (`flutter_web_auth_2`: ASWebAuthenticationSession / Custom Tabs), return to `mise://` app URL set by the server, then poll server status | §23–28; no WebView, no SDK secrets, no IAP. |
| Real-time | SSE over `dio` streamed response, own parser, backoff 1/2/5/15 s, `Last-Event-ID`, closed in background | Existing contract. |
| Push | `firebase_core` + `firebase_messaging`, initialised only when a Firebase config is supplied at build time; otherwise disabled | No Firebase project yet (Blocked). |
| Images | `cached_network_image` with bounded memory cache | §44. |
| Cooking | `wakelock_plus` only while cooking mode is on | §32. |
| Fonts | Young Serif, Instrument Sans (SIL OFL) bundled as assets | Same brand as web. |
| Identifiers | Proposed `tt.mise.customer` (+ `.dev`, `.staging` flavours via `--dart-define` and Android `applicationIdSuffix`), display name "Mise" | Not permanent until the owner approves (§53). |
| Mobile auth | First-party access + rotating refresh tokens (§2 of the audit) | No OAuth server exists; PKCE not applicable. |

## 2. Backend additions (Go, migration `00023`)

1. `mobile_refresh_tokens` + endpoints `POST /auth/mobile/login`, `POST /auth/mobile/refresh`, `POST /auth/mobile/logout`. Access
   token = a `customer` session with a 15-minute expiry; refresh token 30-day rolling, rotated on every use; reuse revokes the family
   and its session. Device name and installation id shown in `/me/sessions`.
2. Checkout/subscribe `client: "app"` → server uses `MOBILE_PAYMENT_RETURN_URL` (validated config, custom scheme or https).
3. `push_devices` + `POST /me/devices`, `DELETE /me/devices/{installationId}`; removed at mobile logout; sender interface with an
   FCM HTTP v1 adapter (tested against a fake), disabled without credentials.
4. `customer_notifications` + `GET /me/notifications`, `POST /me/notifications/{id}/read`, `POST /me/notifications/read-all`; written by
   the worker from the outbox events that already send customer emails (order, payment, renewal, delivery, chat reply).
5. `GET /me/orders/{id}/meals/{mealId}/recipe`: published customer instructions for that week, only for the order's owner.
6. `account_deletion_requests` + `GET/POST/DELETE /me/account/deletion-request` and an admin read-only queue.
7. OpenAPI for all of the above; Go tests incl. negative/security tests (expired access token, revoked/reused refresh token,
   customer token on admin intelligence routes, foreign order/recipe/conversation, invalid device token).

## 3. App feature map

Tabs: **Home**, **Menu**, **My Box**, **Support**, **Account**. Outside the shell: onboarding, auth screens, meal detail, plans,
checkout, payment result, order detail, delivery tracking, cooking mode, conversation, help article, notifications.

## 4. Testing strategy

- Unit: serialization, auth state and refresh (single-flight, reuse → signed out), payment outcome mapping, selection state, subscription
  state, notification/deep-link routing allow-list, chat dedup by `client_message_id`/`seq`, offline cache and sign-out wipe.
- Widget: every screen listed in brief §55 with a fake API, including loading and error states.
- Integration (`integration_test`): the critical journey against the local API, on the Android emulator, run only with the owner present.
- Backend: Go tests as above, plus the existing suite (no regressions).
- Screenshots: rendered from the real widgets in tests (labelled as test renders), and on the emulator against the local API when it
  can be run. iOS: Blocked (no macOS).

## 5. Machine safety rules (this PC powers off under heavy load)

Never `flutter doctor`. `PUB_CACHE=E:\FULL MISE DEV\.pub-cache`, `GRADLE_USER_HOME=E:\FULL MISE DEV\.gradle`. `flutter test
--concurrency=1`. Gradle: `org.gradle.workers.max=1`, `org.gradle.parallel=false`, `-Xmx1536m`. One heavy job at a time, never
alongside Docker builds or Playwright. Android builds and the emulator only after asking the owner.

## 6. Beta and release

Android: debug + release APK/AAB build scripts, signing from environment/keystore outside git (none exists → unsigned or debug-signed
artifacts only). iOS: project configured, build Blocked (no macOS). Store listings, data safety and privacy disclosures derived from the
actual SDKs and data used. Nothing is published.

## Progress

- [x] Stage 1 — audit (`SPRINT_10_AUDIT.md`) and this plan
- [x] Stage 2 — Flutter project, design system, navigation shell, environments (analyze clean, unit tests 15/15)
- [x] Stage 3 — mobile auth backend (migration 00023, Go tests pass) + auth screens and session handling
- [x] Stage 4 — home, catalogue (search, filters, pagination, offline copy), meal detail, server favourites (favourites list screen lands with Account in Stage 9)
- [x] Stage 5 — plans, subscription management (server-allowed actions only), weekly selection (version conflicts, cutoff), My Box
- [x] Stage 6 — checkout and payments: `client: "app"` with server-set MOBILE_PAYMENT_RETURN_URL (Go tests pass), hosted page in system browser session, server-verified outcome polling
- [x] Stage 7 — orders, delivery tracking (real milestones, no GPS), cooking mode; backend `GET /me/orders/{id}/meals/{mealId}/recipe` (owner + confirmed order + published version only; Go tests pass)
- [x] Stage 8 — support chat (existing inbox; SSE with Last-Event-ID replay, backoff, closed in background; dedup by seq and client id) and help centre (plain text only)
- [x] Stage 9 — migration 00024 + internal/appnotify (in-app centre from email events, push devices, FCM v1 adapter tested on a fake, deletion requests + admin queue; Go tests pass), app notification centre, push service (off until Firebase), account screens, validated deep links
- [ ] Stage 10 — security, performance, accessibility review
- [ ] Stage 11 — tests, screenshots, Android build, docs, report, commit
