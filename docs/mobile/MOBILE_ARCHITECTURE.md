# Mise app: architecture

The customer app for iOS and Android (Sprint 10), in `apps/mobile`. Flutter 3.38.7 / Dart 3.10.7. Plan and audit:
`docs/architecture/SPRINT_10_PLAN.md`, `SPRINT_10_AUDIT.md`.

## Principles

- **The Go API is the only source of truth.** The app shows what the server says (prices, totals, availability, deadlines,
  statuses, allowed actions) and sends the customer's choices. It never computes a price, decides eligibility or confirms a payment.
- **One customer, one record.** The app uses the same account, subscription, favourites, addresses and conversations as mise.tt.
- **Native, not a WebView.** Provider payment pages open in the system browser session; legal pages in an in-app browser tab.
- **No customer AI, no admin surface.** The app has no Mise Intelligence screens; mobile tokens are customer tokens and cannot open
  any `/admin` route (tested on every admin route).

## Layout

```text
apps/mobile/lib/
  main.dart                 → app/bootstrap.dart
  app/                      bootstrap, environment (dart-defines), providers, router (go_router), tab shell
  core/
    api/                    ApiClient (Dio): envelope, errors, idempotency keys, safe logging, JSON helpers
    authentication/         SessionManager (keystore tokens, single-flight refresh), AuthInterceptor, AuthApi
    errors/                 AppError: every failure as a safe, human message
    notifications/          deep links (app_links + allow-list), push (FCM, off until configured), crash reporting hook
    realtime/               SSE parser and reconnecting connection
    security/               SafeLinks: the allow-list for every path from outside the app
    storage/                SecureStore (credentials), Flags (non-personal), CacheStore (offline copies)
  design_system/            tokens, typography, theme, components
  features/                 onboarding, authentication, home, catalogue, meal_details, plans, subscriptions,
                            meal_selection, my_box, checkout, orders, delivery, cooking, support, notifications, account
  shared/                   models (catalogue, commerce, orders), formatting, media URLs, small widgets
```

Each feature has its API calls (`*_api.dart`), its state where it needs one (Riverpod `Notifier`/`FutureProvider`), and its
screens. Pure logic that matters is kept out of widgets and unit-tested: `SelectionDraft` (meal choices), `MessageLog` (chat
dedup), `pollOutcome`/`outcomeCopy` (payments), `SafeLinks`, `routeForNotification`, `SessionManager`.

## Technology decisions

| Need | Choice | Notes |
|---|---|---|
| State | `flutter_riverpod` 3.3.2, no code generation | |
| Navigation | `go_router` 17.5.0, `StatefulShellRoute` (5 tabs keep their stacks) | Android back and iOS swipe work by default; predictive back on Android. |
| HTTP | `dio` 5.11.1 | Interceptor adds the bearer token and refreshes once on 401. |
| Models | Hand-written immutable classes | No Freezed/json_serializable/Drift: `build_runner` is too heavy for the development PC, and the model set is moderate. Unknown fields are ignored (API v1 only adds fields). |
| Credentials | `flutter_secure_storage` 11.2.0 | Keychain (this device only, after first unlock) / Android Keystore. |
| Offline | JSON files in app-private storage (`path_provider`) | Excluded from backups; account area wiped at sign-out. |
| Payments | `flutter_web_auth_2` 5.1.0 | ASWebAuthenticationSession / Custom Tabs; never a WebView. |
| Links | `app_links` 7.0.0 | Flutter's own deep linking is switched off so every link is validated. |
| Push | `firebase_core` 5.1.0, `firebase_messaging` 17.0.1 | Initialised only with `MISE_PUSH=true` and a Firebase config (none yet). |
| Images | `cached_network_image` 3.4.1 | Decoded at display size. |
| Cooking | `wakelock_plus` 1.6.1 | Screen kept on only while cooking mode is open. |
| Other | `url_launcher`, `package_info_plus`, `connectivity_plus`, `shared_preferences`, `uuid`, `intl`, `flutter_native_splash`, `flutter_launcher_icons` (dev) | Versions pinned exactly in `pubspec.yaml`. |

All are published by Flutter, Firebase or widely used maintained packages under BSD/MIT/Apache-2.0 licences; the in-app licence page
lists them. 63 newer major versions exist that are incompatible with Dart 3.10's resolution today; upgrades are a separate change.

## Environments

`--dart-define`s, read once at start-up (`app/environment.dart`): `MISE_ENV` (local, development, staging, production),
`MISE_API_BASE_URL`, `MISE_SITE_URL`, `MISE_MEDIA_BASE_URL`, `MISE_APP_SCHEME`, `MISE_DEEP_LINK_HOST`, `MISE_PUSH`,
`MISE_CRASH_REPORTING`. Staging and production must use https; production refuses development hosts and never shows test payment
methods even if the server lists them. Android flavors `dev` (`tt.mise.customer.dev`, scheme `mise-local`, cleartext only to
10.0.2.2), `staging` (`.staging`, `mise-staging`), `prod` (`tt.mise.customer`, `mise`). The identifiers are **proposed** and need
the owner's approval before any store registration.

## Start-up

Native splash (brand ground and wordmark), then: read the environment, read flags and the keystore (local, fast), render. Nothing
waits for the network: the profile loads in the background, and push starts 300 ms after the first frame. A misconfigured build shows
a plain error instead of starting.

## Backend additions made for the app

| Change | Where |
|---|---|
| Mobile token sign-in, refresh rotation with reuse detection, logout (migration 00023) | `internal/identity/mobile.go` |
| Checkout `client: "app"` with a server-set app return URL (`MOBILE_PAYMENT_RETURN_URL`) | `internal/commerce/checkout.go` |
| Customer recipe endpoint (approved steps for a bought box) | `internal/commerce/recipe.go` |
| Notification centre, push devices, FCM sender, deletion requests (migration 00024) | `internal/appnotify` |

All are in `docs/api/openapi.yaml` (1.13.0) and covered by Go tests (`internal/server/mobile_*_test.go`).

## Building on the development PC

This PC powers off under heavy load and its C: drive is nearly full. Never run `flutter doctor`. Pub and Gradle caches live on E:.
`android/gradle.properties` caps Gradle at a 1.5 GB heap with one worker and no daemon (Flutter's template asked for 8 GB). Run
`flutter test --concurrency=1`, and one heavy job at a time. iOS cannot be built here (no macOS).
