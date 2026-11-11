# Authentication

How people prove who they are to Mise. Decision record: `docs/decisions/ADR-003-authentication.md`.
Code: `services/api/internal/identity`.

## Three kinds of credential

| Credential | Who | Where it lives | Lifetime | Grants |
|---|---|---|---|---|
| Lead token (HMAC, stateless) | Waitlist leads | Email links; the `profile` token in the marketing site's `localStorage` | verify 7 d, profile 30 d, unsub never | One lead's referral page and preferences. **Never** account access. |
| Customer session | Customers | `mise_session` cookie on the customer app's server | 30 d absolute, 7 d idle | Customer endpoints (`/api/v1/me`, `/auth/logout`) |
| Admin session | Staff | `mise_admin` cookie on the admin app's server | 12 h absolute, 2 h idle; 10 min while MFA is pending | Admin endpoints, by permission |

The API rejects a token presented for the wrong audience (tested in
`TestSessionAudiencesAreSeparate` and `TestAdminAuth`).

## Sessions

- Token: 32 bytes from `crypto/rand`, base64url. Stored as SHA-256 only (`sessions.token_hash`).
- Checked on every request: not revoked, before `expires_at`, used within the idle limit,
  user `active`, and still holding the access the audience needs (`admin.access`, or the
  `customer` role). Removing a role or disabling a user ends access on the next request.
- `last_seen_at` is written at most once a minute per session.
- Revocation reasons are recorded: `logout`, `user_revoked`, `password_reset`,
  `admin_revoked`, `account_disabled`, `mfa_failed`, `mfa_reset`. Ended sessions are kept
  for 30 days, then deleted.
- Customers see and end their own sessions (`GET/DELETE /api/v1/me/sessions`). Operators
  end anyone's with `misectl admin signout`.

## Web: backend-for-frontend, cookies and CSRF

- The browser talks only to the Next.js app it loaded. Server actions and route handlers
  on that app call the API with the bearer token from the cookie. `connect-src 'self'`
  in both apps' CSP.
- Cookie policy:

  | | Admin (`mise_admin`) | Customer (`mise_session`) |
  |---|---|---|
  | `HttpOnly` | yes | yes |
  | `Secure` | yes (off only with `ADMIN_COOKIE_SECURE=false` locally) | yes (`WEB_COOKIE_SECURE=false` locally) |
  | `SameSite` | `Strict` | `Lax`, so someone arriving from an email link arrives signed in |
  | `Path` | `/` | `/` |
  | `Max-Age` | the API's `expires_in` | the API's `expires_in` |

- **CSRF.** The API never reads cookies and allows no credentialed CORS, so it has no
  CSRF surface. On the apps, every state change is a Next.js server action, which accepts
  only `POST` from the app's own origin (Origin/Host check) and is not reachable by a
  cross-site form; `SameSite` adds a second layer. Sign-out is a server action too, never a `GET`.
- Pages that receive a token in the URL (`/verify-email`, `/reset-password`) send
  `Referrer-Policy: no-referrer` and need a button press, so link scanners in mail
  systems cannot use up a single-use token.

## Client address (rate limits behind a BFF)

Requests from a Next.js server all come from that server's address, which would turn
per-address limits into one global limit. Each app therefore sends
`X-Mise-Client-Ip` and `X-Mise-Client-Ua` with `X-Mise-Bff-Key`. The API believes them
only when the key matches `BFF_SHARED_KEY` (constant-time compare); otherwise it uses the
socket or `X-Forwarded-For` as before. Tested in `TestBFFForwardedClientAddress`.

## Customer flows

- **Register** `POST /auth/register`: 202 `check_email` whatever the outcome. A new
  address gets a verification email; an address with an unverified account gets a new
  link; a verified account's owner is emailed "you already have an account". Nothing in
  the response reveals which. Closed (403 `registration_closed`) unless
  `CUSTOMER_REGISTRATION_ENABLED`.
- **Verify** `POST /auth/verify-email`: single use. For customers, also links the
  account to the waitlist lead with the same canonical address (`DATA_FLOW.md`).
- **Sign in** `POST /auth/login`: a wrong password, an unknown address, a disabled
  account and a staff-only account all answer 401 `invalid_credentials`, and unknown
  addresses cost the same Argon2 work. An unverified address gets 403
  `email_not_verified` (only after the right password) and a fresh link.
- **Forgot / reset**: always 202; a reset link (1 h, single use) goes only to active
  customers **without** `admin.access`. Resetting sets the password, invalidates other
  reset links, ends every session and marks the address verified.

## Admin sign-in and MFA

1. `POST /admin/auth/login` checks the password and that the account holds `admin.access`.
2. The answer carries `mfa`:
   - `verified` / `not_enrolled`: a full session (`not_enrolled` only where MFA is not required, i.e. local).
   - `challenge`: a 10-minute session that may call only `me`, `logout` and `mfa/verify`.
   - `enrolment_required`: the same, with `mfa/setup` (secret, otpauth URL, QR code) and `mfa/confirm`.
3. On a correct code the session token is **replaced** (session fixation defence) and the
   session becomes full for the normal TTL.

TOTP details: 6 digits, 30 s, SHA-1 (universal app support), ±1 step of drift, each step
accepted once (`mfa_totp.last_used_step`). Five wrong codes end the session
(`mfa_locked`). Secrets are sealed with AES-256-GCM under `DATA_ENCRYPTION_KEY`, with the
user id as associated data. `ADMIN_MFA_REQUIRED` defaults to true outside `local` and the
API refuses to start in production with it false.

## Account recovery

| Situation | Customer | Staff |
|---|---|---|
| Forgotten password | Self-service email reset | `MISE_ADMIN_PASSWORD=… misectl admin create -email …` sets a new one and ends sessions |
| Lost authenticator | n/a | `misectl admin reset-mfa -email …`; they enrol again at next sign-in |
| Suspected compromise | `DELETE /me/sessions` | `misectl admin signout` / `admin disable` |

Staff recovery is never by email alone: a compromised inbox must not lead to admin access.

## Passwords

Argon2id, m=19 MiB, t=2, p=1, 16-byte salt, PHC string. 12 characters minimum, 256 bytes
maximum. Hashes with absurd parameters are refused before hashing. Not yet done (tracked in
THREAT_MODEL): a breached-password check.

## Mobile (Flutter), designed for later

Not built in Sprint 1. The intended flow, which needs no schema change:

1. `POST /api/v1/auth/mobile/login` returns an access token (15 min) and a refresh token
   (30 d), both opaque; the session row has `client = 'mobile'`.
2. The app keeps the refresh token in the Keychain / Android Keystore, never in plain storage.
3. `POST /api/v1/auth/mobile/refresh` rotates the refresh token on every use; reuse of an
   old one revokes the whole session (theft detection).
4. Logout and "sign out everywhere" work as on the web because they act on `sessions`.

## Configuration

`ADMIN_SESSION_TTL`, `ADMIN_SESSION_IDLE_TTL`, `ADMIN_MFA_REQUIRED`,
`CUSTOMER_SESSION_TTL`, `CUSTOMER_SESSION_IDLE_TTL`, `CUSTOMER_REGISTRATION_ENABLED`,
`CUSTOMER_APP_URL`, `TERMS_VERSION`, `DATA_ENCRYPTION_KEY`, `BFF_SHARED_KEY`. See `.env.example`.
