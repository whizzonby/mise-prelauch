# Sprint 2 report: customer web platform

Written 11 November 2026. The work is on branch `sprint-2/customer-platform`, built on
Sprint 1 (`fc03e57`), and is **not yet committed**. The plan and decisions are in
`docs/architecture/SPRINT_2_PLAN.md`.

## 1. What customers can do now

- **Account:** register, confirm their email, sign in and out, reset a forgotten
  password, and change their password or email while signed in.
  - Changing the email needs the current password, a link sent to the new address and a
    notice to the old one.
  - They can see and end their other sessions.
  - Pages behind sign-in send a visitor to sign in and back again. The return path is
    checked against open redirects.
- **Waitlist link:** a verified waitlist lead who registers with the same address is
  linked to their account.
  - Their dietary answers, menu interests, household size and launch-email consent carry
    over once, and only into records the customer has not filled.
  - Allergies are never carried over.
  - The lead, its referrals and its history are untouched.
- **Onboarding:** seven mobile-first steps: welcome, who you cook for, meal interests,
  dietary preferences, allergies, location, done.
  - Each answer is saved as it is given, and the position is saved too, so onboarding
    resumes on any device.
  - Back keeps what was answered, and optional questions can be skipped.
  - Contradictory diets are refused with a reason.
- **Dashboard:**
  - a greeting in Trinidad and Tobago time
  - a prompt to finish setup
  - an editorial hero
  - meals suggested from the customer's own preferences, each with its reasons
  - categories
  - a chef spotlight
  - a brand story

  No orders, deliveries or subscription figures are shown, because none exist.
- **Meals:**
  - **Browsing:** search; filters for category, dietary needs, cooking time, protein and
    chef; "hide meals with my allergens"; sorting; pages. All filtering happens in the
    database, one page at a time.
  - **Meal page:** photograph, chef, times, servings, the ingredients in the kit,
    allergens (only what is declared, always with the cross-contact notice), nutrition
    (only once verified), the cooking overview, and a favourite button.
- **Favourites:** saved instantly on screen, with rollback if the save fails. A meal
  can't be saved twice. Unpublished meals disappear from the list without being lost.
- **Account settings:** profile, food preferences, allergies (a separate form, with the
  time they were last confirmed), household, addresses (several, one default, with
  delivery availability), email preferences (account and security email always on;
  optional topics recorded as consent), and sign-in and security.
- **Staff (admin, Meals section):**
  - list meals by status; create drafts; edit with protection against two people saving
    over each other; publish and unpublish
  - add categories and chef profiles
  - nutritionists enter and verify nutrition figures
  - each step is limited to the roles allowed to do it

## 2. Screens and routes

Customer app (`apps/web`):

| Kind | Routes |
|---|---|
| Signed out | `/sign-in` (with `/login` redirecting to it), `/register`, `/check-email`, `/verify-email`, `/forgot-password`, `/reset-password`, `/confirm-email-change` |
| Onboarding | `/onboarding` |
| Signed in | `/dashboard`, `/meals`, `/meals/[slug]`, `/favorites`, `/chefs/[slug]`, `/profile`, `/profile/preferences`, `/profile/household`, `/profile/addresses`, `/profile/notifications`, `/profile/security` |

Admin app (`apps/admin`): `/meals`, `/meals/new`, `/meals/[id]`, `/meals/setup`.

Subscription, orders, deliveries, billing and rewards are deliberately absent from
navigation and return 404 until they are built.

Screenshots at 1440 px and 390 px are in `docs/screenshots/sprint-2/`.

## 3. Backend modules

- **New:**
  - `internal/vocab`: shared value lists (dietary keys, allergens, proteins, meal
    interests, Trinidad and Tobago regions, email topics)
  - `internal/catalog`: public catalogue, admin, recommendations, demo seed
  - `internal/addresses`
  - `internal/favorites`
  - `internal/delivery`: the eligibility interface over `delivery_zones`
- **Extended:**
  - `internal/customers`: profile, household, preferences and allergies, onboarding,
    notifications, waitlist import (one file per concern)
  - `internal/identity`: password change, email change, `SetName`, four new account emails
  - `internal/leads`: the read-only `ImportableFor`
  - `internal/notifications`: the new email templates
  - `cmd/misectl`: `catalog seed-demo`
  - config: `CATALOG_DEMO_ENABLED`, `CATALOG_IMAGE_HOSTS`

## 4. Migrations

`00005_customer_platform.sql` is additive and reversible. The details are in
`docs/database/SPRINT_2_SCHEMA.md`.
- **Tested on a scratch database:** up, then down twice to version 3, then up again.
- **`misectl doctor`** confirms every permission the code checks exists in the database.

## 5. API endpoints (40 new, all in `docs/api/openapi.yaml`, version 1.2.0)

- **Customer (22):**
  - `GET|PATCH /me/profile`
  - `PUT /me/password`
  - `POST /me/email`, `POST /auth/confirm-email-change`
  - `GET|PUT /me/household`
  - `GET|PUT /me/preferences`
  - `GET|PUT /me/onboarding`
  - `GET|PUT /me/notification-preferences`
  - `GET|POST /me/addresses`, `PATCH|DELETE /me/addresses/{id}`
  - `GET|POST /me/favorites`, `DELETE /me/favorites/{mealId}`
  - `GET /me/recommendations`
- **Public (6):** `GET /meals`, `/meals/{slug}`, `/meal-categories`, `/chefs`,
  `/chefs/{slug}`, `/reference`.
- **Admin (12):**
  - `/admin/catalog/meals`: list and create
  - `/admin/catalog/meals/{id}`: read and update
  - `/admin/catalog/meals/{id}/publish` and `/unpublish`
  - `/admin/catalog/meals/{id}/nutrition`
  - `/admin/catalog/categories`: list and create
  - `/admin/catalog/chefs`: list, create and update

**Deviation:** the profile is at `/me/profile` (`GET`/`PATCH`) rather than `PATCH /me`.
`GET /me` stays the identity endpoint ("who is signed in"), so its Sprint 1 contract
doesn't change.

## 6. Authentication and security changes

- **Object-level authorisation:**
  - Every `/me/*` endpoint takes the customer from the session.
  - Address and favourite queries are keyed by both the record id and the customer id,
    so another person's id answers 404, exactly like a missing one.
  - Drafts, unpublished meals and demo content (where it is off) answer 404.
- **New permissions:**
  - `catalog.read`, `catalog.manage`, `catalog.publish` and `nutrition.verify`.
  - Nutritionists now get console access, to verify nutrition.
  - The chef role was deliberately **not** given console access.
- **Password and email changes:**
  - Both need the current password and are rate-limited per account and per address.
  - A password change ends every other session and emails a notice.
  - An email change is enumeration-safe: if the new address already has an account, that
    address is told and the requester is not.
- **Audit:** profile changes (field names only), household, preferences, allergies,
  consent, addresses, onboarding completion, waitlist import and every catalogue action
  are audited.
  - Allergy entries record that allergies changed and how many there are, never which
    ones. A test checks this.
- **No secrets in the browser:** tokens stay on the Next.js server, and the browser
  bundles hold no keys.
- **Test runs:** `RATE_LIMITS_DISABLED` (refused outside local) now also lifts the
  per-account limits. Repeated test runs had been locking out the test admin.

## 7. Design-system components

They are built on `packages/ui`, with no new visual language:
- **Forms (`components/forms.tsx`):** `Form` (server-action form with shared field
  errors and a "saved at" message), `TextField` (with a show/hide password button),
  `SelectField`, `ChoiceField`, `CheckboxField`, `SubmitButton`, `FormMessage`.
- **Meals:** `MealCard`, `MealGrid`, `FavoriteButton` (optimistic, with rollback),
  `DemoTag`, `EmptyState`, `SectionHeading`.
- **Navigation:** the desktop top navigation and the phone bottom tab bar (`app-nav.tsx`),
  and `ProfileNav`.
- **Meal filters:** `FilterForm` (a GET form, so it works without JavaScript and every
  view has a URL) and `FilterPanel` (filters fold away on phones).
- **Other:** `OnboardingFlow`, `AllergyFields`, `AddressFields`.

The decisions on stack choices (server actions instead of React Hook Form and TanStack
Query, CSS motion, no shadcn layer) are in `docs/decisions/ADR-006-customer-app-frontend.md`.

## 8. Tests executed (11 November 2026)

| Check | Result |
|---|---|
| Go `gofmt`, `go vet`, build | clean |
| Go tests, database tests against real PostgreSQL | **86 pass**, 0 skipped. New: session audience on every `/me` route; profile; password change; email change (including the enumeration-safe path); household and preferences validation; contradictory diets; allergies never touched by other saves, and kept out of the audit log; onboarding resume and completion; consent records; the waitlist import (once, never overwriting, unsubscribed leads excluded); address ownership, default and limit; published-only catalogue; demo content hidden; filters and search; nutrition shown only when verified; catalogue permissions; version conflict; favourites; recommendations that respect diets and allergies |
| Migration 00005 up, down, up | pass |
| sqlc and OpenAPI-generated code current | pass |
| ESLint, `tsc` (all workspaces) | pass |
| Vitest | **38 pass** (marketing 22, admin 6, web 10, including safe redirects, Trinidad and Tobago time and wording that never promises delivery). Run one suite at a time: run together on this PC, the marketing suite once found no tests because memory was short |
| Next.js builds (web, admin) | pass |
| Playwright, against the running stack with the demo catalogue | **31 pass**. The 7 new journeys passed three times in a row. New: onboarding (saves, reload resume, contradiction, Back, completion); dashboard and filters, search, meal page, save and unsave; addresses, default, email preferences, household validation; deep link through sign-in and an open-redirect attempt; admin create, publish refused without a photograph, publish, unpublish; the phone tab bar with no sideways scroll; axe WCAG 2.2 A/AA scans of 10 customer pages. All 24 earlier tests still pass |
| Visual review | Screenshots of 10 screens at desktop and phone widths, looked at. Two fixes came from it: phone filters folded away; a smaller meal title |

**Not tested:**
- the Docker image build (npm network problems on this PC this week)
- GitHub Actions (there is no remote)
- Safari, Firefox and physical phones
- screen readers by hand
- load
- Lighthouse

## 9. Known issues and limitations

- **Real menu:** all meals, categories and the chef profile are **demo content**.
  Nothing real can be shown until the kitchen supplies recipes and photographs.
- **No photograph uploads:** images are files in `apps/web/public/catalog/` or on an
  approved host. Uploads need object storage, which is a cost decision.
- **Basic meal editor:** photographs and ingredients are one per line in a text box.
- **Delivery zones:** these have no admin screen yet (SQL only), so every address shows
  "availability pending".
- **Household:** counts only. Preferences per person are not modelled yet; the tables
  allow it later.
- **Not carried over from the waitlist:** the "chef-series" and "school-lunch" menu
  interests have no equivalent in the new list.
- **Recommendations:** they rank at most 200 published meals. That's fine at this
  catalogue size; revisit later.
- **No account deletion or data export for customers yet.** Leads have erasure (Sprint 1).
- **Phone numbers:** they aren't verified. The SMS and WhatsApp channel choices are
  stored for later; nothing sends by them yet.
- **Dev-only advisory:** one high advisory remains in `vite` (the unit-test runner only;
  see the Sprint 1 report, F1).

## 10. Business decisions (answered 11 November 2026)

1. **Customer registration: open.**
   - `CUSTOMER_REGISTRATION_ENABLED=true` in `.env.production.example` and in the
     production and staging Terraform. The code's default stays off, so an environment
     must switch it on explicitly.
   - The customer app itself is not yet deployed to AWS. That is a fifth Fargate service,
     about US$9–12 a month per environment (ADR-005), and still needs a go-ahead.
2. **Real menu, chef profiles, photographs and nutritionist sign-off: to be supplied.**
   - Staff enter them in the admin (Meals).
   - The demo catalogue stays out of production.
3. **Waitlist consent mapping: confirmed.** Launch consent turns on *weekly menu* and
   *promotions*, recorded with `source = waitlist_consent`.
4. **Delivery area: all of Trinidad and Tobago.**
   - Migration `00006_delivery_zones_tt.sql` makes all 15 regions active, so addresses
     and profiles now say "Mise delivers here".
   - To pause a region, set its `delivery_zones.status` to `planned` or `unavailable`.
   - A test fails if a region is added without a zone.
5. **Photo storage on S3: approved.**
   - `infrastructure/terraform/modules/platform/storage.tf` adds a private, encrypted,
     versioned bucket.
   - It is served only through the existing site CloudFront distribution at
     `https://<domain>/media/*`, with no new distribution or certificate.
   - The site's host is added to `CATALOG_IMAGE_HOSTS` automatically.
   - Estimated under US$1 a month for storage and requests at launch volume.
   - Uploading from the admin is **not built yet**, so no service has write access.
     Operators copy files in with `aws s3 cp` (the commands are in `storage.tf`).
   - **Not applied:** applying the Terraform needs your AWS account, and your go-ahead
     at that time.

## 11. Sprint 3 readiness

**Ready:**
- **Meals:** stable ids, slugs and versions that weekly menus can reference.
- **Customers:** households (servings), dietary needs and allergies (to warn at
  selection), addresses with a default and a delivery-eligibility interface,
  favourites to choose from, and email topics already reserved for subscription
  reminders and delivery updates.
- **Staff:** console permissions to extend with `subscriptions.manage` and `orders.manage`.

**Needed before Sprint 3 can finish:**
- pricing and plans
- the payment provider, with its API documentation and sandbox credentials
- the delivery partner's integration details
- delivery zones
- the real menu

Sprint 3 has not been started.
