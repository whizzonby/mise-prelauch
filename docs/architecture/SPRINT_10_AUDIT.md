# Sprint 10 audit: the customer mobile app

Audited 21 November 2026 (PC clock; the clock on this machine has drifted before) on branch `sprint-10/mobile-app`, cut from
`fb04028` (Sprint 9.2, final). Brief: `E:\FULL MISE DEV\Sprint 10`. Method: reading the code, migrations, OpenAPI and earlier reports.
No mobile code existed before this sprint.

## 1. What exists and is reused

The Go API (`services/api`) is the only source of truth. The app calls it directly at `/api/v1` (the web apps are BFFs; a native
app is not). Contracts written for this sprint in Sprint 9.1: `docs/api/FLUTTER_CONTRACTS.md`.

| Need | Endpoint(s) | Status |
|---|---|---|
| Register, verify email, sign in, sign out, reset password | `POST /auth/register`, `/auth/verify-email`, `/auth/login`, `/auth/logout`, `/auth/password/forgot`, `/auth/password/reset` | Reusable. Login returns an opaque session token (hashed in `sessions`), audience `customer`. |
| Device sessions | `GET /me/sessions`, `DELETE /me/sessions/{id}` | Reusable for "signed-in devices" and "sign out everywhere". |
| Profile, household, preferences, notification choices, addresses | `/me`, `/me/profile`, `/me/household`, `/me/preferences`, `/me/notification-preferences`, `/me/addresses[/{id}]` | Reusable; shared with the web app (one record). |
| Favorites | `GET/POST /me/favorites`, `DELETE /me/favorites/{mealId}` | Reusable, server-backed (brief §18 already met). |
| Catalogue | `GET /meals` (published only; filters incl. `exclude_allergens`), `/meals/{slug}`, `/meal-categories`, `/reference`, `/chefs` | Reusable. `MealDetail` carries description, cooking overview, images, ingredients, verified-only nutrition, chef. |
| Weekly menus | `GET /weekly-menus`, `/weekly-menus/{week}` | Reusable. |
| Plans and pricing | `GET /plans`, `/plans/{slug}`, `/subscription-info`, `POST /me/subscriptions/quote` | Reusable; prices come from the server. |
| Subscription actions | `/me/subscription`, `POST /me/subscriptions`, `…/{id}/change-plan|pause|resume|cancel|address` | Reusable. Only these actions exist; the app offers exactly these. Skipping weeks *is* `pause` ("skips 1 to max_pause_weeks weeks, from the first week not yet paid"); there is no separate skip action. |
| Weekly selection | `GET /me/subscription-cycles`, `GET/PUT /me/meal-selections/{cycleId}`, `…/finalize` | Reusable; server validates quantity, availability and the cutoff. |
| Orders, billing, invoices | `/me/orders[/{id}]`, `/me/billing`, `/me/invoices[/{id}[/pdf]]` | Reusable. |
| Delivery tracking | `GET /me/orders/{id}/delivery`, `…/delivery/support`, `…/delivery/reschedule-requests` | Reusable; milestones are real statuses, no GPS, no ETA unless given. |
| Payments | `GET /payment-options`, `POST /checkout/sessions`, `GET /checkout/sessions/{id}` (server polls the provider), webhooks | Reusable, with one change (§2.2). |
| Support | `/chat/availability`, `/me/chat/...` and `/chat/guest/...` incl. SSE `…/stream`, `/me/chat/link-guest` | Reusable; same inbox as the web widget. SSE, not WebSocket. |
| Help centre | `GET /help/categories`, `/help/articles`, `/help/articles/{slug}` | Reusable (published only; plain text bodies). |
| Analytics | `POST /behavior/events` (Sprint 9.2, versioned, consent-aware) | Reusable for app events. |

**Authorization boundary.** Every `/admin/...` route (373 in OpenAPI) uses `RequireAdmin`, which authenticates with audience `admin`.
A customer token is looked up with its own audience, so it cannot open an admin route. Mise Intelligence is entirely under `/admin`.
Sprint 10 adds explicit tests that mobile customer tokens get `401` on intelligence, finance and AI routes.

## 2. Missing mobile capabilities (backend additions)

1. **Mobile tokens (brief §13).** Today a customer session lasts until idle/absolute expiry, with no refresh token, and the token is
   meant for a BFF cookie. Add first-party mobile auth: short-lived access token (a `customer` session row with a short expiry) plus
   a rotating refresh token, stored hashed, grouped in a family; reuse of a spent refresh token revokes the whole family. Device name
   and installation id recorded; sessions listed and revocable through the existing `/me/sessions`. Web cookies and CSRF untouched.
   PKCE/OAuth is not used: the identity system has no authorization server, and adding one is out of scope for a first-party app.
2. **Checkout return to the app (§23–26).** `ReturnURL` is server-set to the web `/subscribe/result`. Add a `client: "app"` choice on
   checkout and subscribe; the server then uses the configured app return URL (`MOBILE_PAYMENT_RETURN_URL`). The client never
   supplies a URL. The return is never proof of payment: the app polls `GET /checkout/sessions/{id}`.
3. **Push devices and sender (§37).** No device registry and no push provider. Add `push_devices` (customer, installation, platform,
   token, updated, revoked), `POST/DELETE /me/devices`, logout cleanup, invalid-token removal, and a sender with an FCM HTTP v1
   adapter behind an interface (credentials server-side only).
4. **In-app notification centre (§39).** No customer notification history exists (emails only). Add `customer_notifications`,
   written from the same outbox events that send emails, with list, mark read, mark all read and deep-link targets.
5. **Cooking content (§32).** Customers see only `cooking_overview`. Kitchen steps (`recipe_steps`, kinds preparation/assembly)
   are internal. The customer-facing content is `recipe_versions.customer_instructions` and `customer_cook_minutes` on the
   *published* version. Add `GET /me/orders/{id}/meals/{mealId}/recipe`: only for a customer whose order contains the meal, only the
   published version in force for that delivery week. No equipment list exists in the data, so none is shown.
6. **Account deletion request (§41).** No workflow exists. Add a request record (create, view, withdraw) and an admin queue; actual
   erasure waits for the retention policy (go-live blocker 23c and legal approval 22). Never presented as completed deletion.

## 3. Payment provider readiness

| Provider | Server | Mobile flow | Status |
|---|---|---|---|
| testpay (local) | Built | Hosted page in an in-app browser tab, return to app, server verification | Verifiable locally |
| PayPal | Adapter built, tested against a fake | Hosted approval (PayPal recommends browser/Custom Tabs, not WebView) | **Blocked**: no sandbox credentials; cannot charge TTD |
| Stripe | Adapter built (Checkout Session), tested against a fake | Hosted Checkout in an in-app browser; native PaymentSheet deferred | **Blocked**: no keys; T&T not a supported Stripe country |
| Scotiabank | Config/capability only | — | **Blocked**: no public specification or merchant credentials |

The app shows only what `GET /payment-options` returns. No Apple IAP / Google Play Billing (physical goods; §27).

## 4. Real-time messaging compatibility

Server-Sent Events with `Last-Event-ID` replay, 15 s pings, ~25 min server close. Works over a bearer token. Native client needs a
streamed HTTP request plus a small SSE parser; close on background, catch up with `messages?after=` on resume. Idempotent sends by
`client_message_id`. No WebSocket exists or is needed.

## 5. Push notification requirements

Firebase project, `google-services.json` / `GoogleService-Info.plist`, an APNs key and a service account are **not available**.
Build everything behind configuration; push stays disabled until they exist (Blocked). Payloads carry ids only, never payment or
account details (lock screen privacy). The in-app centre is authoritative.

## 6. Security considerations

Tokens in Keychain/Keystore via `flutter_secure_storage`; nothing sensitive in shared preferences; no tokens or chat text in logs or
crash reports; deep links validated against a route allow-list and never bypass sign-in; payment return URLs fixed by the server;
customer cache cleared at sign-out; no server secrets in the app; release builds with obfuscation as a secondary measure only.
Open Sprint 9 finding **F1** (the AWS API host exposes customer and admin paths publicly, all authenticated): a native app needs the
customer paths public, so F1 is narrowed to "admin paths should not be reachable from the internet" — still open, infrastructure.

## 7. Mobile design requirements

Reuse the web tokens (`docs/architecture/design-system.md`): cassava `#f3ecdd`, coconut `#fcfaf4`, charcoal `#22211d`, muted
`#5f5b50`, dasheen green `#1e3a2b`, chadon beni `#4a6a28`, roucou accent `#b4411c`, borders `#d9cfba`/`#8a8372`, states
`#2f6b3f`/`#8a5a00`/`#9b1c2e`. Young Serif + Instrument Sans (SIL OFL, bundled). Radii 4/8/12/20. One overlay shadow only. No
gradients, glass or pill shapes. Light theme only. Wordmark "Mise" with a roucou full stop. Photographs: the licensed Pexels set in
`docs/branding/IMAGE_SOURCES.md` (stock, labelled as such) for onboarding; meal photos come from the API.

## 8. Existing business blockers (from `docs/launch/GO_LIVE_CHECKLIST.md`)

No approved domain (deep links/universal links cannot be verified), no merchant accounts or sandbox keys, 0 published non-demo meals
and 0 priced non-demo plans, no legal approval of privacy/terms, chat and data retention undecided, no production environment.

## 9. Platform-specific dependencies (new for Sprint 10)

| Dependency | Status |
|---|---|
| Flutter SDK | 3.38.7 stable, Dart 3.10.7, at `C:\flutter` (read from `flutter.version.json`; `flutter doctor` is not run on this PC, see below) |
| Android SDK | Platforms 31–36, build-tools 34–36, NDK up to 28.2, JDK 21 (Android Studio JBR), emulator image Pixel_7 |
| macOS / Xcode | **None.** iOS cannot be built, signed or run here: Blocked |
| Apple Developer / App Store Connect | **None known**: Blocked (bundle id, APNs, TestFlight) |
| Google Play Console | **None known**: Blocked (internal testing track, signing) |
| Firebase project | **None**: Blocked (push, Crashlytics) |
| App identifiers | None approved. Proposed `tt.mise.customer` with `.dev` / `.staging` suffixes, pending approval |

**Machine constraint.** This PC powers off under heavy load (toolchain checks, large builds) and drive C: has ~7.5 GB free. Work
rules for this sprint: never run `flutter doctor`; Pub and Gradle caches on drive E:; one heavy job at a time
(`flutter test --concurrency=1`, Gradle with one worker and a capped heap); Android builds and the emulator only with the owner present.
