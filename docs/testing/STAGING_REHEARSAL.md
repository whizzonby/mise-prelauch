# Local staging rehearsal (Sprint 9)

Run 25 November 2026 on the developer PC (Docker Desktop 27.3.1). **This is not AWS staging.** No AWS account, domain or production
database was available. The rehearsal checks that the release artifacts and the production configuration work together; it does not
check DNS, certificates, RDS, SES, CloudFront, ECS or the WAF.

## Set-up

- Compose file: `docker-compose.prod.yml` **as shipped**, project `mise-staging`, `MISE_ENV=production`.
- Release labels: `RELEASE_VERSION=staging-rehearsal`, `RELEASE_COMMIT=694b82d` (new build args added to the compose file this sprint).
- Database: a new, empty `mise_staging` database on the local PostgreSQL 17 container (reached at `host.docker.internal:5460`).
- `.env.production` generated from `.env.production.example`: `DOMAIN=mise.localhost`, fresh random `TOKEN_SIGNING_KEY`,
  `DATA_ENCRYPTION_KEY`, `BFF_SHARED_KEY`, SMTP pointed at Mailpit, `CATALOG_DEMO_ENABLED=false`, `PAYMENTS_TEST_PROVIDER=false`.
  The file is gitignored and was deleted after the run.
- **Without the `caddy` profile.** Running it (binding ports 80/443) was not permitted on this workstation, so all app ports were bound
  to `127.0.0.1` only and **TLS termination was not tested**.

## Build

`docker compose … build`: api (also migrate and worker), marketing, admin and web images all built. The network on this PC dropped
several times (npm registry and Docker Hub timeouts); each failure was a fetch error and succeeded on retry.

**Defect found and fixed:** the marketing image build hung and then failed on `/`. The home, FAQ and how-it-works pages read live meals
and plans at build time, but the API is not reachable during an image build. Even if the build had succeeded, the image would have
served "The menu could not be loaded" for up to five minutes after every deploy. Fix: those three pages now render per request
(`connection()`), the API reads keep their five-minute cache, and each read gives up after 4 seconds. The customer app was not
affected (its catalogue already renders on demand).

## Results

| Check | Result |
|---|---|
| `migrate` runs first on an empty database | `schema is at version 14`; api and worker started after it |
| `GET /api/v1/version` | `{"version":"staging-rehearsal","commit":"694b82d"}` |
| `/api/v1/health`, `/api/v1/ready` | 200, 200 |
| Worker | `worker started`; heartbeat row `worker@…`, version `staging-rehearsal`, beat < 1 s ago |
| Public pages | `127.0.0.1:3100/` 200 (0.56 s first request), `/faq` 200; `:3102/meals` 200, `/plans` 200, `/sitemap.xml` 200; `:3101/login` 200 |
| Honest empty states with no menu and no plans | home: "The first menu is being written and tested in our kitchen…", "Plans are being finalised…"; API `/meals` and `/plans` return empty lists. No sample or demo content. |
| Security headers (customer app) | CSP, `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy: no-referrer`, `Permissions-Policy`, HSTS |
| Security headers (site) | CSP, `X-Frame-Options: DENY`, HSTS with preload |
| Admin API through the site | `GET :3100/api/v1/admin/launch` → 404 |
| Production refuses the test payment provider | `PAYMENTS_TEST_PROVIDER cannot be true in production: the test provider never takes real money` (api exits) |
| Production refuses the log mailer | `MAIL_DRIVER=log is only allowed when MISE_ENV=local` |
| Production refuses `migrate down` | `migrate down is only allowed when MISE_ENV=local` |
| Production refuses demo content | `refusing to load demo content: CATALOG_DEMO_ENABLED is off` |
| First admin | `misectl admin create -role super_admin` with `MISE_ADMIN_PASSWORD`: created; sign-in returns a token with `mfa`; `GET /api/v1/admin/launch` before MFA → **403 `mfa_required`** |

## Not covered

TLS and certificate issuance (Caddy profile not run), real DNS, AWS (ECS, RDS, SES, CloudFront, WAF), a real email provider, a real
payment provider, rollback between two releases, and load on production hardware. These remain go-live gates
(`docs/launch/GO_LIVE_CHECKLIST.md`).
