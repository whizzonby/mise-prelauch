# Security audit (Sprint 9)

Scope: the Go API and worker, the three Next.js apps (marketing, customer `web`, `admin`), PostgreSQL, the production compose stack and
the AWS Terraform, as of branch `sprint-9/launch-readiness`. References used as checklists: OWASP Top 10 (2021) and OWASP ASVS 4.0
level 2. **This is an internal engineering review, not a certification, penetration test or PCI DSS assessment.** No intrusive
scanning of third-party systems was done.

Threats and controls by area: `THREAT_MODEL.md`. Authentication and authorization design: `AUTHENTICATION.md`, `AUTHORIZATION.md`.

## 1. Automated scans (run 25 November 2026)

| Scan | Command | Result |
|---|---|---|
| Go static analysis | `go vet ./...` (Go 1.27.4) | clean |
| Go vulnerabilities | `govulncheck ./...` | **0 reachable**. 1 advisory in a required module not called by our code: GO-2026-5932 (`golang.org/x/crypto/openpgp`, unmaintained; Mise does not import it). Update `x/crypto` when a tagged fix ships. |
| Node dependencies | `pnpm audit --prod` | No known vulnerabilities |
| Secrets in git history | `gitleaks git` v8.21.2, 36 commits | 4 findings, **all false positives**: fixed test keys in `config_test.go` (3) and a `curl -fsS` health check (1). No real secret committed. `.env*` files are gitignored. |

CI (`.github/workflows/ci.yml`) runs govulncheck, `pnpm audit` and gitleaks on every push, but no CI run results are available
from this workstation (the `gh` CLI is not installed and the Sprint 9 audit found no record of a run), so these local runs are the
evidence.

## 2. Tests that exercise security controls

| Control | Tests |
|---|---|
| RBAC: every role × admin endpoints by direct API call | Go `TestEveryRoleReachesExactlyTheAdminEndpointsItIsGranted` (rbac_matrix_test.go) |
| Customer ownership isolation on every `/me/*` resource with an id | Go `TestCustomersCannotReachEachOthersRecords`, `TestInvoicePDFAndOwnership`, `TestAddressesOwnershipAndDefault` |
| Session expiry, idle timeout, revocation, audience separation | Go `TestSessionsEndWhenIdleOrExpired`, `TestCustomerSessionsListAndRevoke`, `TestSessionAudiencesAreSeparate`, `TestPasswordResetEndsEverySession` |
| Staff MFA and lockout | Go `TestAdminMFALocksAfterRepeatedWrongCodes` and Sprint 1 identity tests |
| Payment webhooks: signature, replay, idempotency, out-of-order | Go `TestWebhooksAreAuthenticatedIdempotentAndOrderIndependent`, `TestCommerceEventsArePublishedOnceWithTheChange` |
| Server-side pricing and promotions | Go `TestPromotionsAreApprovedAndComputedOnTheServer`, `TestQuoteDiscountsAndTax` |
| Open-redirect protection on `next` | Vitest `safeNext` (apps/web `helpers.test.ts`) |
| Forwarded-address spoofing | Go `TestBFFForwardedClientAddress` |
| Production config refuses unsafe settings | Go config tests (test payments, log mailer, MFA off, rate limits off outside local) |

## 3. Review by area

| Area | Finding | Status |
|---|---|---|
| Authentication | Argon2id; uniform responses (no enumeration); per-email and per-address login limits; staff MFA mandatory in production; staff passwords not resettable by email. | OK |
| Session cookies | Session tokens live in the BFFs' httpOnly cookies, `Secure` in production (`WEB_COOKIE_SECURE`, `ADMIN_COOKIE_SECURE`), `SameSite=Lax` (customer app) / `Strict` (admin); stored hashed; idle and absolute expiry. | OK (code review; `Secure` set from env in production compose) |
| CSRF | State changes are Next server actions (same-origin POST, origin-checked by Next) or API calls with bearer tokens; the API ignores cookies. No GET state changes. | OK |
| CORS | API allows only the site origin (`CORS_ORIGINS`); customer and admin traffic is server-to-server. | OK |
| Authorization | Permission checks on every admin route (matrix test); ownership on every customer route (isolation test). | OK, tested |
| Input validation / SQL injection | All SQL through sqlc or parameterised pgx queries; no string-built SQL from input (report filters use bound parameters). | OK (review) |
| XSS / output encoding | React escaping; one `dangerouslySetInnerHTML` for JSON-LD from our own static copy with `<` escaped. CSP on all three apps. | OK |
| SSRF | The API makes no outbound HTTP requests from user input. Catalogue images are restricted by `CATALOG_IMAGE_HOSTS` in `next/image`. | OK |
| File uploads | None in the API (photography referenced by URL from approved hosts). | N/A |
| Rate limiting | Per-process limiters on sign-up, tokens, lookups, events, admin login, general; WAF rate rule in AWS. Per-process limits multiply with instances (accepted, T17). | OK with known limit |
| Password reset / verification | Hashed single-use tokens, 1 h / 48 h expiry, created at send time; reset revokes all sessions. | OK, tested |
| API errors | Uniform JSON errors with `request_id`; no stack traces or SQL errors returned. | OK |
| Secrets | From env / Secrets Manager; production refuses missing keys; nothing in the repo (scan above). | OK |
| Payments | No card data stored or handled: the test provider is the only adapter and is refused in production. Amounts computed server-side; webhooks signed and idempotent; refunds need `payments.refund`. No real provider exists, so live payment security is **not assessed**. | Blocked on provider |
| Logging | No passwords, tokens, TOTP codes, request bodies or query strings logged; customer emails not logged in auth events. | OK (review) |
| Admin API exposure in AWS (T7) | On the ALB, `api.<domain>` forwards every path, so `/api/v1/admin/*` and `/api/v1/me/*` are reachable from the internet. Every route is still authenticated, permissioned and (admin) MFA-gated. Single-server Caddy already exposes only public paths. | **Open, High** — see §5 |
| Breached-password check (T2) | Not implemented. | **Open, Medium** |
| CAPTCHA on registration (T18) | Not implemented; registration rate-limited per address. | **Open, Medium** before public registration |

## 4. Security headers (production builds)

Configured in each app's `next.config.ts`; checked on the production images in the local rehearsal (`docs/testing/STAGING_REHEARSAL.md`,
plain HTTP on loopback; TLS itself not tested):
`Content-Security-Policy` (with `frame-ancestors 'none'`), `Strict-Transport-Security`, `X-Content-Type-Options: nosniff`,
`X-Frame-Options: DENY`, `Referrer-Policy`, and `Permissions-Policy` on the marketing site and customer app.

## 5. Findings to close before launch

| # | Severity | Finding | Recommended fix |
|---|---|---|---|
| F1 | High | T7: customer and admin API paths publicly routable on the AWS API host | Add ALB listener rules on `api.<domain>`: forward only the public paths (health, ready, version, leads, referrals, events, payment webhooks); everything else 404 unless the request carries the BFF key header (ALB `http_header` condition), or move BFF→API traffic to an internal load balancer. Not done in Sprint 9: it cannot be tested without AWS access. |
| F2 | Medium | No breached-password check | k-anonymity range lookup (Have I Been Pwned) at registration and reset; needs a decision to send hash prefixes to a third party. |
| F3 | Medium | No CAPTCHA on registration | Add one (e.g. Turnstile) before opening public registration; needs a provider decision. |
| F4 | Medium | Payment integration not reviewable | Review the real provider's integration (webhook verification, idempotency, refunds, secret storage) when one is chosen. |
| F5 | Low | Error pages do not show the `request_id` | Show it so support can find the log line. |
| F6 | Low | `golang.org/x/crypto` advisory (unreachable) | Update when a tagged fix is released. |

None of these is hidden by the launch control centre: `security_review` stays **Blocked** until F1 is fixed or formally accepted.
