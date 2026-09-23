# Security and privacy

What the platform protects, how, and where the limits are.

## What is at stake

- **Lead data**: names, email addresses, optional phone numbers, preferences. The asset.
- **The admin**: anyone who gets in can read and export all of it.
- **The mail channel**: a signup form that sends email can be turned against third parties.
- **The referral count**: worth gaming if rewards are ever attached.

## Controls

### Input

- Every input is validated on the server (`internal/leads/validate.go`,
  `internal/events/events.go`, `internal/admin/http.go`). Browser validation exists for
  feedback only.
- JSON bodies are size-limited, must be `application/json`, and unknown fields are rejected.
- **SQL injection**: every query uses bound parameters. The one dynamically built query
  (admin lead filters) concatenates only fixed clause text and parameter placeholders;
  search text is escaped for `LIKE`.
- **XSS**: React escapes everything rendered; the only raw HTML is the site's own
  structured-data JSON, with `<` escaped. Email templates use `html/template`. Both apps
  send a Content-Security-Policy. The marketing site is static, so its policy allows
  inline scripts (Next.js needs them without a per-request nonce); it restricts network
  access to the Mise API and forbids framing.
- **Names** are restricted to letters and ordinary name punctuation. The name is echoed in
  email, and a free-text name would let someone use Mise's mail server to deliver their
  own message or link to a stranger.

### Abuse and bots

- Per-address rate limits on every route, tightest on signup (10 an hour). Limits are held
  in process memory, so they multiply by the number of API instances; the AWS WAF
  rate-based rule is the global backstop.
- A honeypot field and a minimum fill time reject most automated form posts.
- Repeat signups for an address send at most one email every ten minutes.
- Referrals count only after the invited person verifies their email, never for the
  referrer's own address (matched on the canonical form, so `+tags` and Gmail dots do not
  help), and are flagged rather than counted when both signups come from the same network.
- Not included: a CAPTCHA. If bot signups appear in the admin, add one (Cloudflare
  Turnstile fits) behind the existing bot checks; nothing else needs to change.

### Authentication and authorisation

- **Leads** hold no password. They prove who they are with HMAC-signed, purpose-bound
  tokens: verification (7 days), profile (30 days), unsubscribe (no expiry). A token for
  one purpose is rejected for another.
- **Admins** sign in with email and password. Passwords are hashed with Argon2id; unknown
  accounts take the same time as known ones. Sessions are 256-bit random tokens stored
  only as SHA-256 hashes, with an expiry.
- The admin app keeps the session token in an `httpOnly`, `Secure`, `SameSite=Strict`
  cookie and calls the API from its server. The token never reaches browser JavaScript.
- Every admin endpoint checks a **permission**, never a role name. Roles map to
  permissions in one place (`internal/admin/auth.go`), which is where the future roles
  (chef, farmer, kitchen staff, finance, …) are added.
- **CSRF**: the public API takes no cookies, so there is nothing to forge. The admin's
  state-changing requests are Next.js server actions, which verify the request origin,
  behind a `SameSite=Strict` cookie.

### Admin-sensitive operations

Sign-ins, status changes, exports and erasures are written to `audit_log` with the actor.
An export's audit entry (who, which filters, how many rows) is written before any data is
sent; if it cannot be recorded, nothing is exported. CSV cells that could be interpreted
as spreadsheet formulas are neutralised.

### Transport and headers

TLS 1.2+ everywhere in AWS, HSTS on all three hosts, `X-Content-Type-Options`,
`X-Frame-Options: DENY`, a restrictive `Referrer-Policy`, and PostgreSQL connections that
require TLS. The admin is marked `noindex` and can be restricted to given IP ranges at the
load balancer (`admin_allowed_cidrs`).

### Secrets

No secret is committed. Local development uses `.env` (git-ignored; `.env.example` holds
only local-only values). In AWS, secrets live in Secrets Manager and are injected into
containers by ECS: the database password (generated and held by RDS), the token signing
key, and the SES SMTP credentials. CI checks every push for committed secrets.

Terraform state contains the generated token signing key and SMTP credentials, so the
state bucket must be private, encrypted and versioned.

### Logs

Structured JSON with a request id on every line. Logged: method, path, route, status,
duration, named events. **Not** logged: query strings, headers, bodies, email addresses,
tokens, passwords, IP addresses. The mail "log" driver records subjects only.

## Privacy

- **Consent** is an unticked checkbox that must be ticked; the time and the wording version
  are stored with the lead.
- **Minimisation**: the signup form asks for a name, an email, an area and consent.
  Everything else is optional. The IP address is never stored, only a keyed hash.
- **Analytics** are first-party, cookie-free, and carry identifiers only, never form
  values. Global Privacy Control and Do Not Track switch them off entirely.
- **Unsubscribe**: a link in every email and a `List-Unsubscribe` header; it takes effect
  immediately, and emails already queued for that lead are dropped at send time.
- **Erasure and access**: an admin with `leads:erase` can delete a lead and everything
  attached; the lead detail page shows everything held about a person.

## Known limits and accepted trade-offs

- **Membership disclosure.** Submitting an address that is already listed returns
  "already on the list". That confirms membership of a food waitlist to whoever submits
  it. It was accepted for a clearer experience; it is rate limited, and no lead data is
  returned. If that is not acceptable, make both outcomes return the same response and
  deliver the referral link only by email.
- **Profile token in `localStorage`.** It unlocks one lead's referral count and profiling
  answers for 30 days. A script injection on the marketing site could read it. The impact
  is small and the CSP limits where a script could send it.
- **Same-network flagging** will occasionally flag a genuine invitation between people in
  one household or office. Flagged referrals are visible in the admin so they can be
  reviewed if rewards depend on them.
- **No multi-factor authentication** on the admin yet. Restrict the admin by IP range in
  production until it is added.
- **No penetration test** has been done.
