# Mise app: notifications

## The notification centre is the authority

Push messages can be delayed, dropped or switched off, so every service notification is also stored and shown in the app's
notification centre (`GET /me/notifications`, mark one or all read). The worker writes entries from the **same events that send
customer emails**, through the existing email senders (`internal/notifications`), so email and app never disagree:

| Event | Type | Opens |
|---|---|---|
| Payment received / failed / due | `payment.succeeded` / `payment.failed` / `payment.due` | Subscription |
| Subscription active | `subscription.renewal` | My Box |
| Paused, resumed, cancelled, plan changed | `subscription.changed` | Subscription |
| Menu out / choose your meals | `selection.reminder` / `selection.deadline` | My Box |
| Meals confirmed | `selection.confirmed` | My Box |
| Delivery scheduled, dispatched, delayed, failed | `delivery.update` | Delivery tracking |
| Delivered | `delivery.delivered` | Delivery tracking |
| Support reply | `chat.reply` | The conversation |

Each entry has a dedup key (the event's identity), so worker retries never create two. Delivery entries follow the same rules as
delivery emails (approved wording, the customer's delivery-updates preference). No marketing notifications are sent.

## Push

- **Devices.** `POST /me/devices` (installation id, platform, FCM token, app version) and `DELETE /me/devices/{installationId}`
  (`push_devices`, migration 00024). A registration is tied to the device session: signing out removes it, and pushes stop for any
  session that has ended (sign out everywhere, password reset). Re-registering the same installation or token replaces the old row
  (token rotation; a token moving to another account). Multiple devices per customer are supported.
- **Sending.** For each new centre entry the worker queues one `app.push` job per live device. The FCM HTTP v1 adapter
  (`internal/appnotify/push.go`) signs a JWT with the service account, caches the access token, and sends. `UNREGISTERED` (404) and
  `INVALID_ARGUMENT` remove the token; other failures are retried by the job runner. A collapse key per notification prevents
  duplicates on the device.
- **Lock-screen privacy.** The push shows only a neutral sentence ("There is an update about a payment. Open Mise to see it.") and
  carries ids (`type`, `link`, `notification_id`). No amounts, plan names, addresses or message text. Android notifications use
  `visibility: PRIVATE`.
- **Credentials** are server-side only: `FCM_SERVICE_ACCOUNT_FILE` (a Google service account JSON) for the worker. Nothing secret is in
  the app.
- **App side.** Firebase starts only with `--dart-define=MISE_PUSH=true` and a bundled Firebase configuration. Permission is requested
  only when the customer turns push on (Account → Notifications). Tapped notifications are routed through the same allow-list as links
  (`routeForNotification`).

## Status

| Part | Status |
|---|---|
| Notification centre (server + app) | **Verified** (Go tests; app widget test) |
| Device registry, logout cleanup, rotation, invalid-token removal | **Verified** (Go tests with a fake pusher) |
| FCM HTTP v1 adapter | **Verified against a fake Google server** (token exchange, caching, error mapping) |
| Real delivery through Firebase/APNs | **Blocked**: no Firebase project, no `google-services.json` / `GoogleService-Info.plist`, no APNs key, no Apple developer account |
| Crash reporting (Crashlytics) | **Blocked**: same Firebase project; the app reports errors to its own log without personal data meanwhile |

To enable: create the Firebase project (Android `tt.mise.customer[.dev|.staging]`, iOS bundle ids), add the configuration files to
`android/app/` and `ios/Runner/` (git-ignored), apply the `com.google.gms.google-services` Gradle plugin, upload the APNs key in
Firebase, set `FCM_SERVICE_ACCOUNT_FILE` on the worker, and build with `MISE_PUSH=true`.

**Known follow-up.** firebase_messaging 17 deprecates registration-token calls in favour of a new installation-id ("direct-send")
model that needs a different server API. The app keeps the token calls (marked) until both sides move together.
