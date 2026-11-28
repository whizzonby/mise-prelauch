# Mise app: authentication

## Why not the web flow, and why not OAuth/PKCE

The web apps are BFFs: a Next.js server holds the session token in an httpOnly cookie, with CSRF protection. A native app has no
server of its own, so it must hold a credential itself. Mise has no OAuth authorization server, and the app is first-party, so a
PKCE flow would add an authorization server for no gain. Sprint 10 adds a first-party token flow instead, without changing the web
flow at all.

## Tokens

| | Access token | Refresh token |
|---|---|---|
| What | Bearer token of a `sessions` row (`audience=customer`, `client=mobile`) | Row in `mobile_refresh_tokens` |
| Life | 15 minutes (`access_expires_at`) | 30 days from issue, single use; the device session ends after 180 days |
| Stored on server | SHA-256 hash only | SHA-256 hash only |
| Stored on device | Keychain / Keystore (`flutter_secure_storage`) | Same |

Endpoints (`docs/api/openapi.yaml`): `POST /auth/mobile/login` (email, password, device name, installation id),
`POST /auth/mobile/refresh`, `POST /auth/mobile/logout` (by refresh token; works with an expired access token). The existing
`POST /auth/logout`, `GET/DELETE /me/sessions` and `DELETE /me/sessions/{id}` work for app sessions too: the device appears with its
name ("Mise app on Android") and can be signed out from the web or the app.

**Rotation and reuse.** Each refresh returns a new pair and makes the old access and refresh tokens worthless. A refresh token that was
already exchanged, presented again, is treated as stolen: the whole device session and every refresh token of it are revoked
(`revoked_reason = refresh_reuse`, audited as `auth.refresh_reuse`), and the app must sign in again. Exchanges lock the row, so two
concurrent refreshes cannot both succeed.

**Revocation.** Signing out, "sign out other devices", a password reset or an admin revoking the account ends the session; refresh
then fails. Push devices registered by that session stop receiving pushes at once (the push job requires a live session) and are
removed on logout.

**Installation id.** A random UUID generated once per install and kept in the keystore. Signing in again on the same installation
ends its earlier session, so a phone never accumulates sign-ins. It is not a hardware identifier.

**Audience separation.** App tokens are customer tokens. Admin routes authenticate with the admin audience, so an app token is
refused on all 445 admin routes, Mise Intelligence included (`TestMobileTokensCannotReachAdminRoutes`). Staff accounts without the
customer role cannot get app tokens.

**Rate limits.** Sign-in shares the customer sign-in limits (per address and per email). Refresh and logout take a 256-bit random
token and use only the general limit, because many phones can share one carrier address.

## In the app

- `SessionManager` loads tokens from the keystore, refreshes when the access token is within 30 s of expiry, and makes refresh
  **single-flight**: concurrent requests wait for one exchange (a second exchange would be reuse and sign the device out).
- `AuthInterceptor` attaches the token and retries a request once after a successful refresh. A refused refresh erases the tokens
  and the account cache, and the app returns to "Your session ended. Please sign in again."
- Offline, an expired access token is kept with its refresh token; the next online request refreshes.
- Screens: sign in, register, check email, forgotten password, reset password, verify email. Errors never reveal whether an account
  exists (the API answers the same for both; registration and reset always say "if … we have sent"). Password manager autofill
  hints are set; `TextInput.finishAutofillContext` saves new credentials.
- Email links (`/verify-email`, `/reset-password`) are accepted by the app only with a well-formed token; the API checks it.

Biometric unlock is not built (optional in the brief): the keystore already requires the device to be unlocked.

## Tests

Go: `internal/server/mobile_auth_test.go` (rotation, reuse, expiry, logout, revocation from the web, same-install sign-in, identical
failure messages, staff refused, admin routes refused). Flutter: `test/core/foundation_test.dart` (single-flight refresh, refused
refresh signs out, offline refresh keeps the session).
