# ADR-003: Authentication and identity

Status: accepted (Sprint 1). Replaces the admin authentication described in
`prelaunch/002` and `internal/admin/auth.go` (removed). `prelaunch/009` (lead tokens) is unchanged.

## Context

Pre-launch, only staff signed in (`admin_users`, `admin_sessions`, roles mapped to
permissions in a Go map, no MFA). The platform now needs customer accounts, and later
chefs, farmers, nutritionists and others, on the web and on Flutter apps.

## Decision

1. **One identity model** for every person: `users` (+ `password_credentials`,
   `sessions`, `auth_tokens`, `mfa_totp`). What a person may do comes from roles, not
   from which table they live in.
2. **Opaque, server-side sessions.** 32 random bytes, base64url; only the SHA-256 hash is
   stored. Sessions have an absolute expiry and an idle limit, can be listed and revoked,
   and are bound to an **audience** (`customer` or `admin`): a customer session never
   reaches an admin endpoint, even for a person who holds both roles.
   Not JWTs: revocation must be immediate (disable, role removal, password reset) and
   there is one API to ask.
3. **Web: backend-for-frontend.** Each Next.js app keeps the token in an `httpOnly`,
   `Secure` cookie on its own origin and calls the API from its server. The browser never
   holds an API token and never calls the account API directly; nothing goes in
   `localStorage`.
4. **Mobile (designed, not built):** the Flutter apps will use the same `sessions` table
   with `client = 'mobile'`: a short-lived access token plus a rotating refresh token kept
   in the platform keystore, issued by `/api/v1/auth/mobile/*` endpoints. See
   `docs/security/AUTHENTICATION.md`.
5. **Passwords**: Argon2id (OWASP parameters), 12+ characters, at most 256 bytes. The
   hashes copied from `admin_users` verify unchanged.
6. **Email tokens** for verification (48 h) and reset (1 h) are single-use, stored hashed,
   and created by the worker at send time, so they never sit in the jobs table.
7. **MFA**: TOTP (RFC 6238, `github.com/pquerna/otp`), secrets sealed with AES-256-GCM.
   Required for every account holding `admin.access` outside local development; it
   cannot be switched off in production. Once someone has enrolled, they are always asked.
8. **RBAC in the database** (`roles`, `permissions`, `role_permissions`, `user_roles`),
   with optional scope (`scope_type`, `scope_id`) on assignments for farmer and kitchen
   contexts. Handlers check permissions, never role names.
9. **Staff recovery is an operator action** (`misectl`), never email-only.

## Consequences

- Existing admins keep their passwords (hashes copied with the same user ids) but sign
  in once more after deploy, and enrol an authenticator at that sign-in.
  **This changes how existing users authenticate, so deploying it needs approval.**
- `admin_users` / `admin_sessions` stay until a later, approved migration removes them.
- OAuth providers can be added as a `user_identities` table without touching passwords.
