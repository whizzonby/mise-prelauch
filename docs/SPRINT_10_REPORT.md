# Sprint 10 report: the Mise customer app (Flutter, iOS and Android)

Date: 28 November 2026 (real date; the development PC's clock reads 21 November). Branch `sprint-10/mobile-app` from `fb04028`
(Sprint 9.2). Brief: `E:\FULL MISE DEV\Sprint 10`. Status words: **Verified**, **Implemented, not verified**, **Blocked**, **Deferred**.

**Outcome.** The customer app is built in `apps/mobile` with every customer feature in the brief, on the existing Go API, plus four
backend additions the app needed. Code, server behaviour and screens are verified by automated tests (57 Flutter, full Go suite) and
test renders of every screen. **Not verified:** any run on an Android device or emulator, any iOS build, real payments, real push.
The Android build stopped before compiling because the PC clock is seven days behind (TLS certificates look not yet valid).
**The app is not release-ready**; see §35–37.

## 1. Repository audit findings
`docs/architecture/SPRINT_10_AUDIT.md`. The API already served catalogue, plans, subscriptions, weekly selection, orders, delivery
tracking, payment options and checkout status, chat with SSE, help centre, favourites and profile. Missing for an app: mobile tokens,
an app payment return, push devices, an in-app notification centre, customer cooking content, and an account deletion request.

## 2. Mobile architecture — Verified (tests)
`docs/mobile/MOBILE_ARCHITECTURE.md`. Feature-first Flutter app; Go API authoritative for every price, rule and status.

## 3. Technology and dependency decisions — Verified (analyze/test)
Riverpod 3.3.2, go_router 17.5.0, Dio 5.11.1, flutter_secure_storage 11.2.0, flutter_web_auth_2 5.1.0, app_links 7.0.0, Firebase
core 5.1.0 / messaging 17.0.1, cached_network_image, wakelock_plus and others, pinned exactly. Freezed/json_serializable/Drift not
used (build_runner is too heavy for this PC; hand-written tolerant models instead). Deviation: the new Go package `appnotify` uses
hand-written SQL, not sqlc.

## 4. Design system — Verified (renders)
`docs/mobile/DESIGN_SYSTEM.md`. Web tokens, Young Serif + Instrument Sans bundled, 20+ components, light theme only, no gradients.
Icon and splash are **proposals** derived from the wordmark (brand approval needed).

## 5. Authentication — Verified
`docs/mobile/AUTHENTICATION.md`. Migration 00023; 15-minute access tokens, single-use rotating refresh tokens (hashed), reuse
revokes the device, device sessions listed and revocable, logout by refresh token, keystore storage, single-flight refresh in the app.
Web cookies and CSRF unchanged. Biometrics: **Deferred** (optional).

## 6. Customer onboarding — Verified (widget test)
Three licensed stock photographs (labelled as stock), skip, sign in, browse as a guest without an account.

## 7. Home and catalogue — Verified (widget tests)
Subscriber home leads with the next box and deadline; guests see plans and the menu. Menu: this week's menu, all meals with search,
category, dietary, allergen-exclusion and time filters (server-side), pagination, pull to refresh, offline copy. No popularity
figures, no recommendations from AI.

## 8. Meal details — Verified (widget test)
Hero photograph, facts, description, declared allergens (never "free from"), verified nutrition only, ingredients, menu
availability, favourite, plan/selection action.

## 9. Subscription management — Verified (widget + Go)
Plans with configured prices; view, pay a due week, change plan, change address, skip weeks (pause), resume, cancel — only the
actions the server allows, each explaining its consequence.

## 10. Weekly meal selection — Verified (unit + widget)
Allowance, per-meal limits, allergy notes, deadline, unsaved-changes state, save and confirm, 409 version conflict and closed window
reload the server copy; offline keeps the draft. Nothing is shown as saved before the server accepts it.

## 11. My Box — Verified (widget test)
Next box, choices, deadline, plan, payment status, address with delivery availability, order and delivery links, other weeks.

## 12. Checkout — Verified server-side; app flow Implemented, not verified on a device
`docs/mobile/PAYMENTS.md`. Server quote, address and availability, payment method, hosted page in the system browser, server
verification by polling. New `client: "app"` with `MOBILE_PAYMENT_RETURN_URL` set by the server (Go tests).

## 13. PayPal integration — Blocked
Adapter exists (Sprint 9.1); no sandbox credentials; PayPal cannot charge TTD.

## 14. Stripe integration — Blocked
Adapter exists; no keys; Trinidad and Tobago is not a Stripe country. Native PaymentSheet, Apple Pay, Google Pay: **Deferred**.

## 15. Scotiabank integration — Blocked
No public specification or merchant credentials; no mobile flow invented.

## 16. Order history — Verified (widget test)
Current and previous orders, detail with meals, payment and billing, address, help. Receipts/invoice PDF in the app: **Deferred**
(invoices remain on the website).

## 17. Delivery tracking — Verified (widget test)
Server milestones, confirmed date and window, delivery code, corrected-outcome notice, no live map or invented ETA.

## 18. Cooking experience — Verified
New `GET /me/orders/{id}/meals/{mealId}/recipe`: only for the customer's own confirmed order, only the published customer
instructions for that week (Go tests prove kitchen steps never leak). App: ingredient checklist, steps, cooking mode with large type,
screen kept on only while open, customer-set timer, offline copy. Equipment lists: **Deferred** (not in approved data).

## 19. Customer support — Verified (unit + widget); live behaviour on a device not verified
`docs/mobile/REALTIME_SUPPORT.md`. Same Mise Chat inbox; SSE with replay, backoff, background close; dedup; retry; read receipts;
typing; feedback; help centre. Attachments: **Blocked** (no approved storage). Guest chat in the app: **Deferred** (website only).

## 20. Push notifications — Infrastructure Verified; delivery Blocked
`docs/mobile/PUSH_NOTIFICATIONS.md`. Migration 00024: notification centre from the same events as emails, device registry bound to
sessions, FCM HTTP v1 sender (tested against a fake Google), neutral lock-screen text. No Firebase project or Apple account.

## 21. Account management — Verified (widget tests)
Profile, addresses, dietary needs and allergies, notification settings, signed-in devices, favourites, privacy, legal and licences,
sign out. Email and password change remain on the website (**Deferred**).

## 22. Offline behaviour — Verified (widget + unit)
`docs/mobile/OFFLINE_STRATEGY.md`. Saved copies labelled; no offline payments, selections, subscription or address changes; cache
wiped at sign-out.

## 23. Mobile analytics — Deferred
No events are sent from the app yet. The Sprint 9.2 `/behavior/events` contract exists; wiring it needs consent handling in the app,
which is not designed. Nothing is collected meanwhile.

## 24. Security assessment
`docs/security/MOBILE_SECURITY.md`. Open: Sprint 9 F1 (restrict admin paths at the load balancer), real-device checks, pen test.

## 25. Admin AI isolation — Verified
Mobile tokens get 401 on all 445 admin routes including every Mise Intelligence route (`TestMobileTokensCannotReachAdminRoutes`,
`TestMiseIntelligenceIsAdminOnly`). The app has no AI, no intelligence screens, no AI keys.

## 26. Accessibility — Implemented, partly verified
Labels, headers, live regions, 48 dp targets, text scaling to 200 % (render at 160 % on a 360 pt phone without overflow errors),
reduced motion, contrast from the web palette. TalkBack/VoiceOver manual checks: **not done** (no device run).

## 27. Performance — Not measured
Images decoded at display size, paginated catalogue, single-flight refresh, no work before first frame. No profile-mode measurement
on devices; no performance claims are made.

## 28. Android verification — Blocked this session
`flutter build apk --debug --flavor dev` failed after 8 s at dependency download: `PKIX path validation failed … validity check
failed`. Cause verified: the PC clock reads 21 November 2026, the real date is 28 November, and `repo.maven.apache.org`'s certificate
is valid from 27 November. No compilation was reached. Gradle is capped (1.5 GB heap, 1 worker, no daemon; Flutter's template asked
for 8 GB). Emulator and device runs were not done at the owner's choice.

## 29. iOS verification — Blocked
No macOS/Xcode. Bundle id, display name, URL scheme, background push mode and deep-link settings are set in the project, unbuilt.

## 30. Automated test results — Verified
`docs/testing/MOBILE_TEST_RESULTS.md`. `flutter analyze`: no issues. `flutter test --concurrency=1`: 57 passed. `go vet ./...`:
clean. `go test -p 1 -count=1 -timeout 30m ./...`: 22 packages ok. New Go tests: `internal/server/mobile_auth_test.go`,
`mobile_checkout_test.go`, `mobile_recipe_test.go`, `mobile_notifications_test.go`.

## 31. Actual application screenshots
`docs/screenshots/sprint-10/` (28 PNGs covering items 2–25 of the brief's list). **These are test renders** of the real widgets with
fixture data, not device screenshots. Device screenshots: **Blocked** with §28. Native splash: assets only.

## 32. Beta build artifacts — Blocked
No APK/AAB produced (§28); no signing keys; no Play or TestFlight accounts.

## 33. App Store readiness — Blocked
`docs/launch/IOS_RELEASE_CHECKLIST.md`.

## 34. Google Play readiness — Blocked
`docs/launch/ANDROID_RELEASE_CHECKLIST.md` (data safety draft from actual collection), `docs/launch/MOBILE_BETA_RUNBOOK.md`.

## 35. Outstanding business dependencies
Approved app identifiers and name; brand approval of the icon and splash; Google Play and Apple developer accounts; signing keys;
Firebase project and APNs key; approved domain (links, `assetlinks.json`, AASA); payment merchant accounts and sandbox keys;
Scotiabank specification; legal approval of privacy, terms and the data retention/deletion policy; published non-demo meals, recipes
with customer instructions, and plan prices; help articles; support staffing (all Sprint 9 go-live blockers still apply).

## 36. Known limitations
No device verification; no iOS build; guest chat, receipts PDF, email/password change, analytics and biometrics are in the web or
deferred; push uses FCM registration tokens (deprecated client calls, to migrate with the server); Universal/App Links not set up;
no integration test of the full journey on a device; the PC clock must be corrected before any build that downloads dependencies.

## 37. Final release recommendation
**Do not release.** Next: correct the PC clock, run the capped Android debug build, then (with the owner present) one emulator run of
the journey against the local stack and real screenshots. Release preparation then waits on §35 (accounts, signing, Firebase,
domain, payments, legal). No store submission without the owner's explicit approval.

## Commit and working tree
Implementation commit and hash note: see the end of this file.
