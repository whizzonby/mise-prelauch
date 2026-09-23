# Status: what has been verified, and what has not

Written at the end of the initial build (23 September 2026). Update it when something on
the "not yet" list is done.

## Verified by running it

| What | How |
|---|---|
| Go API: signup, verification, duplicates, validation, bot checks, rate limiting, referrals (pending, converted, flagged), milestone email, preferences, unsubscribe, events, CORS, admin auth, permissions, lead list and filters, lead detail, status change, CSV export and its audit entry, erasure | 27 Go tests, 15 of which drive the HTTP handler end to end against a real PostgreSQL 17 database |
| Email is actually sent and readable | Worker → SMTP → Mailpit, checked by hand and by the end-to-end suite, which reads the verification link out of the delivered email |
| Marketing site components and content rules | 21 Vitest tests: waitlist form behaviour, analytics and attribution, content integrity, the analytics taxonomy matching the API allow-list, and "no gradients anywhere" |
| Admin helpers | 6 Vitest tests |
| The whole product in a browser | 17 Playwright tests, passing against both the dev servers and the production Docker images: visitor → signup → email → verified → welcome page → profiling; invitation link → signup → referral credited only after verification; unknown invitation code; invalid input; duplicate signup; admin closed to visitors; admin sign-in → dashboard → search → lead detail → status change → CSV download → sign-out; mobile menu; no sideways scroll on a phone; reduced motion; the pinned story on desktop; keyboard-only form use; axe WCAG A/AA scans of four pages; SEO essentials |
| Lint and types | ESLint and `tsc` clean across all workspaces; `gofmt` and `go vet` clean |
| `docker compose up --build --wait` | Builds all images and brings the full stack up healthy |
| Terraform | `terraform fmt -check` and `terraform validate` pass for the staging root (same module as production) |
| Layout | Looked at in screenshots at 1440 px and 390 px wide |

## Not yet exercised

These are built but have not been run for real. Treat each as unproven until it has been.

- **AWS.** Nothing has been applied. The Terraform validates, but `terraform plan` has not
  been run against an account, so resource-level mistakes (a wrong argument AWS rejects, a
  quota, a naming collision) are still possible. The deploy script and workflow have never
  executed.
- **GitHub Actions.** The CI and Deploy workflows have not run; the repository has no
  remote yet. Every command in CI was run locally, but the workflow files themselves are
  untested.
- **SES.** Sending through SES (STARTTLS, authentication) uses the same code path as
  Mailpit but has only been run against Mailpit, which has neither.
- **Real devices and browsers.** Checked in Chromium only. Not checked in Safari or
  Firefox, on a physical phone, or at tablet and very large desktop widths.
- **Screen readers.** Automated axe scans pass; no manual pass with VoiceOver, NVDA or
  TalkBack has been done.
- **Performance.** No Lighthouse or Core Web Vitals measurement. The choices that should
  make it fast are in place (static pages, optimised images, CSS animation, lazy GSAP),
  but nothing has been measured.
- **Load.** No load test. Rate limits are per API instance.
- **Backup restore.** RDS backups are configured in Terraform; a restore has not been rehearsed.
- **Error tracking.** Errors are logged and alarmed on in CloudWatch. There is no
  dedicated error tracker (Sentry or similar).

## Placeholders to replace before launch

- **Photography**: stock photographs from Pexels (`docs/content/image-credits.md`).
- **Sample menu**, chef copy and plan descriptions: `apps/marketing/src/content/`.
- **Legal text**: `content/legal.ts` is a draft and needs legal review, including the
  retention period and response times it states.
- **Domain**: `mise.tt` is a stand-in throughout (Go module path, example configuration, email sender).
- **Git identity**: commits so far are authored as "Your Name"; set `user.name` and `user.email`.

## Decisions for the business

None of these block the build; each is a configuration or content change.

1. The production domain and the email sending address.
2. Whether email verification stays on (it is on; it protects the referral count and the list's quality).
3. Whether invitations earn anything. Nothing is promised on the site today. Milestone emails exist and are off (`REFERRAL_MILESTONES`).
4. Whether "already on the list" may be shown to someone who submits an existing address (see `docs/architecture/security.md`, "Known limits").
5. Whether the admin should be reachable only from given IP ranges (`admin_allowed_cidrs`).

## Deliberately not built

Per the brief: customer accounts, ordering, payments, subscription management, chef and
farmer tools, inventory, the mobile app. The architecture leaves room for them
(`docs/architecture/plan.md`, "Future-platform review").

Also not built, and worth adding next: multi-factor authentication for admins, an admin
"change password" screen, a CAPTCHA behind the existing bot checks, event-level funnels on
the dashboard, and an OpenAPI description to generate clients from.
