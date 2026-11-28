# Sprint 10: mobile test results

Run on 28 November 2026 (real date; the PC clock read 21 November, seven days behind) on Windows 10, branch `sprint-10/mobile-app`.
Flutter 3.38.7 / Dart 3.10.7. Go tests in `golang:1.27-alpine` against PostgreSQL in the `mise-s1` compose project.

## Summary

| Suite | Command | Result |
|---|---|---|
| Flutter static analysis | `flutter analyze` | **No issues** |
| Flutter unit + widget tests | `flutter test --concurrency=1` | **57 passed, 0 failed** |
| Screen renders (golden) | `flutter test --concurrency=1 test/widget/screens_test.dart` | **25 passed**, 28 renders match |
| Go vet | `go vet ./...` | **Clean** |
| Go full suite | `go test -p 1 -count=1 -timeout 30m ./...` | **22 packages ok** (`internal/server` 306 s), 0 failed |
| Android debug build | `flutter build apk --debug --flavor dev --dart-define=MISE_ENV=local` | See §Android build |
| iOS | — | **Blocked**: no macOS/Xcode |
| Integration tests on a device/emulator | — | **Not run**: the owner chose not to run the emulator on this PC this session |

## Flutter tests (57)

- `test/core/foundation_test.dart` (15): link allow-list and external link mapping; notification routing; error envelope parsing and
  internal-detail hiding; environment safety (https, no development host in production, no test payments in production); money
  formatting; single-flight token refresh; refused refresh signs out and erases tokens; offline refresh keeps the session.
- `test/features/commerce_test.dart` (11): model parsing (tolerates unknown fields; unverified nutrition hidden); subscription
  actions from the server; payment outcome mapping (unknown is never success); pending copy warns not to pay again; polling stops at
  the first final outcome and survives a network blip; meal selection draft (allowance, per-meal limit, closed week, conflict
  reset); query equality.
- `test/features/support_test.dart` (6): SSE parser (ids, multi-line data, pings); reconnect backoff; chat dedup by seq and client id;
  failed send keeps its id and a retry does not duplicate; help articles stay plain text.
- `test/widget/screens_test.dart` (25): onboarding; sign-in error that reveals nothing; registration validation; subscriber and guest
  home; menu, weekly menu, server-side filters; meal detail with allergens; plans; weekly selection (allergy warning, unsaved state);
  My Box (test-payment label); checkout (server total, test method label); payment result from the server; orders and order detail;
  delivery tracking (no live map); cooking mode; support home and conversation; help centre; notification centre; account and
  settings; offline saved copy labelled; error state hides internals; guests redirected to sign-in; sign-out wipes the account cache;
  large text on a small phone renders without overflow errors.

## Go tests added for Sprint 10

`internal/server/mobile_auth_test.go`, `mobile_checkout_test.go`, `mobile_recipe_test.go`, `mobile_notifications_test.go`:
rotation and reuse revocation, access expiry, logout and revocation from the web, identical failure messages, staff refused,
**mobile tokens refused on every admin route (Mise Intelligence included)**, app checkout return set by the server and verified
server-side, refused clients create nothing, duplicate subscribe refused, foreign checkout 404, recipe only for the owner of a
confirmed order and published content (kitchen steps never leak), notification centre from real events without duplicates, neutral
push text, push stops after logout, token rotation and invalid-token removal, deletion request rules, FCM adapter against a fake
Google (token exchange and caching, unregistered/invalid mapping, private Android visibility). RBAC matrix: 26 roles × 445 admin
routes.

## Security and negative cases (brief §57)

| Case | Where verified |
|---|---|
| Expired access token | Go `TestMobileAccessTokenExpiresAndRefreshes` |
| Revoked refresh token | Go `TestMobileLogoutAndRevocation` |
| Refresh token reuse | Go `TestMobileLoginRefreshRotationAndReuse` |
| Unauthorized order access | Go `TestCustomerRecipeOnlyForABoughtBox`, existing `TestCustomersCannotReachEachOthersRecords` |
| Unauthorized conversation access | Existing Sprint 9.1 chat tests (404 for others' conversations) |
| Customer access to admin AI endpoints | Go `TestMobileTokensCannotReachAdminRoutes`, `TestMiseIntelligenceIsAdminOnly` |
| Invalid payment return URL | Go `TestAppCheckoutReturnsToTheAppAndIsVerifiedServerSide` (client value refused; URL server-set) |
| Duplicate payment submission | Go (second subscribe 409); app idempotency key and busy button |
| Interrupted checkout | App always verifies with the server after the browser closes; unit test of polling |
| Invalid notification deep link | Flutter `notification routing` tests |
| Unavailable meal / expired deadline | Server rules (existing Sprint 3 tests); app handles `selection_closed` and `version_conflict` by reloading |
| Unsupported delivery address | Availability shown from the server (`delivery_availability`); server refuses at subscribe |
| Network loss during selection / messaging | Unit tests (draft kept; chat retry dedup); offline widget test |
| Logout with cached personal data | Widget test `sign-out clears cached personal data` |

## Android build

See the result recorded in `SPRINT_10_REPORT.md` §28 (the build ran after this document was first written).

## Not done (and why)

- **Emulator and real-device runs** (integration journey, push, deep links, payment return in Custom Tabs, TalkBack): not run this
  session at the owner's choice, because heavy workloads power this PC off. Status: **Implemented, not verified** on devices.
- **iOS**: Blocked (no macOS).
- **Performance profiling** in profile/release mode on representative devices: not done; no claims are made about smoothness.
- **End-to-end journey (brief §56)** across app, admin and kitchen: the server half is covered by Go tests (checkout, production
  demand from paid orders, delivery events, chat to the admin inbox); the app half by widget tests with fixtures. A single run of the
  whole journey on a device against the local stack has **not** been done.
