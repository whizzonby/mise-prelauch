# Mise app: security review (Sprint 10)

Reviewed against OWASP MASVS v2 categories. Status words: **Verified** (tested), **Implemented** (built, not tested on a device),
**Blocked**, **Open**.

| MASVS area | Control | Status |
|---|---|---|
| STORAGE | Tokens only in Keychain (this device only, after first unlock) / Android Keystore-backed storage; never in preferences or logs | Implemented (unit-tested with a fake store) |
| STORAGE | Offline copies in app-private storage, excluded from backup and device transfer; account copies wiped at sign-out | Verified (widget test for the wipe) |
| STORAGE | No card data, provider secrets, push credentials or AI keys in the app | Verified (code review: no secret-bearing defines; payment pages are provider-hosted) |
| CRYPTO | TLS for staging and production (https enforced at start-up); Android cleartext only to 10.0.2.2 in the dev flavor; system CAs only | Implemented |
| AUTH | Short-lived access tokens, rotating single-use refresh tokens, reuse revokes the device, server-side session records, logout and sign-out-everywhere | Verified (Go tests) |
| AUTH | Customer tokens refused on all 445 admin routes, Mise Intelligence included; staff accounts cannot get app tokens | Verified (Go tests) |
| AUTH | Identical answers for wrong password and unknown email; registration and reset never confirm an account exists | Verified (Go + widget tests) |
| NETWORK | Certificate pinning | Not used (optional in the contract; adds outage risk while certificates are unmanaged). Revisit after launch. |
| PLATFORM | Deep links and notification payloads validated by an allow-list; never bypass sign-in; API re-checks ownership | Verified (unit + widget tests) |
| PLATFORM | Payment pages in the system browser session, never a WebView; return URL set by the server; return never trusted | Verified (Go tests for server side) |
| PLATFORM | No exported components beyond the launcher and the payment callback; main activity accepts only known link hosts | Implemented |
| PLATFORM | Push shows neutral text only; Android notification visibility PRIVATE | Verified (Go test of payload) |
| PLATFORM | Clipboard: chat text is selectable by the customer; the app never writes secrets to the clipboard | Implemented |
| CODE | Dependencies pinned; release builds minified and resource-shrunk (R8); obfuscation optional and not relied on | Implemented |
| CODE | Errors shown as plain sentences; server internals never displayed; crash reports carry error type and stack top only | Verified (unit + widget tests) |
| RESILIENCE | Root/jailbreak detection, tamper checks | Not built (not required for a meal-kit app; server-side authorization is the control) |
| PRIVACY | Analytics: none sent from the app yet (see report §23); support attachments disabled; no camera/photo permissions | Implemented |

## Server changes reviewed

- `mobile_refresh_tokens` and session columns store hashes only; reuse detection under row locks; audit entries for login,
  logout and reuse.
- `MOBILE_PAYMENT_RETURN_URL` validated at start-up; `client` limited to `web`/`app`; refused before anything is created.
- Recipe endpoint: owner + confirmed order + meal in the order + published version; kitchen steps, costs and suppliers excluded.
- `push_devices` bound to a live session; tokens never logged; invalid tokens removed.
- Deletion requests: customer-scoped, audited, admin listing requires `customers.read`; no automatic erasure.

## Open items

- Sprint 9 finding F1 (admin API paths reachable from the internet on AWS, all authenticated) is **Open**: with the app calling the
  API host directly, customer paths must be public, so the fix is to restrict `/api/v1/admin/*` to the admin origin at the load
  balancer.
- Real-device checks (screenshots in the app switcher, backup exclusion, keystore behaviour after OS restore) are not done:
  **Blocked** on devices/emulator runs (see the test results document).
- Penetration test of the token endpoints before launch.
