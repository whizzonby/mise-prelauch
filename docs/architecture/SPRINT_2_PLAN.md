# Sprint 2 plan: customer web platform

Written 11 November 2026, on branch `sprint-2/customer-platform` from Sprint 1 (`fc03e57`).

## Step 1: Sprint 1 verification

Re-checked against the code, not the report:

- **Auth:**
  - Customer sessions are opaque tokens in an httpOnly `mise_session` cookie, held by the
    `apps/web` server (BFF). The API checks them with `identity.Service.RequireCustomer`.
  - Register, verify, sign-in, sign-out, forgot, reset, `me` and the session list all
    exist and are tested.
- **Lead link:**
  - `customers.lead_id` is set only after email verification, never for blocked leads.
  - The lead row is untouched, and the link is recorded as an event.
- **RBAC:** permissions are in the database, and a test ties the code's permission keys
  to the migration.
- **Router:** Go `net/http` ServeMux with method patterns, not chi. It is documented in
  ADR-001, and Sprint 2 keeps it.
- **Tests:** all green on 11 November 2026 (Go 69, Vitest 31, Playwright 24/24, on
  Next.js 16.4.6).
- **Gaps that block Sprint 2 (fixed in this sprint):**
  - `/me` cannot be changed (no `PATCH /me`).
  - There is no password change while signed in.
  - There is no email change.
  - `auth_tokens.purpose` allows only two purposes.
- **Not blocking:** a few things that Sprint 1 did and the brief would do differently:
  - The customer app uses Next.js server actions and `useActionState`, not React Hook
    Form or TanStack Query.
  - There is no shadcn layer; the Mise components in `packages/ui` sit directly on Radix.

  Sprint 2 keeps both (see "Deviations").

## Decisions

| Topic | Decision |
|---|---|
| Route names | Keep Sprint 1's `/sign-in`; `/login` redirects to it. `/` sends a signed-in customer to `/dashboard` and anyone else to `/sign-in` |
| Customer data ownership | Every `/me/*` endpoint takes the customer from the session, never from the URL or body. Address and favourite ids are always matched together with the session's `customer_id`, so another person's id answers 404 |
| Names | The person's name stays in `users` (identity). The phone number, contact channel and language go on `customers`. `PATCH /me` calls identity for the name and customers for the rest, in one transaction |
| Email change | Re-enter password, then a single-use link goes to the **new** address and a notice to the old one. The address changes only when the link is used. A new `auth_tokens` purpose (`change_email`) carries the pending address |
| Preferences versus allergies | Kept in separate tables. Allergies are never written by onboarding defaults or by the lead link. `allergies_reviewed_at` tells "no allergies" apart from "not answered". Every allergy change is audited (keys only, no free text in the audit entry) |
| Lead preferences | On the lead link, dietary preferences and household size are copied into the customer's own records **only if those are still empty**, marked `source = 'waitlist'`, and recorded in the audit log. The link happens only after email verification. Allergies were never asked on the waitlist and are never copied |
| Marketing consent | Topics: `account_security` (always on, cannot be turned off), `meal_updates`, `weekly_menu`, `promotions`, `referral_updates`. Optional topics default **off**. If a linked lead gave launch consent and has not unsubscribed, `weekly_menu` and `promotions` start on, with `source = 'waitlist_consent'`. Every change is stored with its source and audited |
| Catalogue versus recipes | A `meal` is what customers see: name, story, photographs, tags, a listed ingredient set, declared allergens, verified nutrition. Production recipes (quantities, yields, costing, versions) are a separate future Chef ERP module that will **reference** a meal. Nothing here models production |
| Allergen claims | `meals.allergen_status` is `unconfirmed` (the default) or `declared`. The site says which allergens a meal *contains*. It never says a meal is *free of* anything, and always shows the cross-contact notice |
| Nutrition | `meal_nutrition.status` is `pending` or `verified`. Only verified figures are returned. Verifying needs the new `nutrition.verify` permission (the nutritionist role) |
| Demo content | `is_demo` on meals, categories and chefs. `CATALOG_DEMO_ENABLED` (on in `local`, off by default elsewhere) decides whether demo rows are visible or publishable. `misectl catalog seed-demo` loads demo content. The demo chef is "Mise Kitchen (demo)", not a person. The photographs are the pre-launch stock images, with their credits |
| Images | Meal images are served by the customer app from `/catalog/*` (static files) or from a host on an allow-list. There are **no uploads** this sprint: S3 storage is a cost decision (ADR-005). Images have alt text, width and height, and a credit |
| Delivery zones | `delivery_zones` table plus a Go `Eligibility` interface. With no zones configured, every address is `unknown` and the app says "availability pending". Nothing promises delivery |
| Personalisation | Deterministic scoring: dietary match, preferred categories, cooking time within the stated limit. Meals containing a declared allergen the customer listed are excluded, with a note that this is not a safety guarantee. No ML, and it is never called "AI" |
| Admin publishing | New permissions: `catalog.read`, `catalog.manage` (drafts), `catalog.publish`, `nutrition.verify`. The admin app gets a small Meals section: list, create, edit, publish, unpublish, categories, chefs |

## Deviations from the brief's preferred stack (and why)

- **React Hook Form and TanStack Query:** not added. Forms post to server actions, so they
  work without client JavaScript and keep the session token on the server. Validation runs
  on the server (Go is authoritative) and shows field errors with the existing `Field`
  component. Favourites use React's `useOptimistic`, which rolls back if the action fails.
  Adding the two libraries would duplicate this.
- **Motion:** CSS transitions with the design-system durations, and none under
  `prefers-reduced-motion`. The onboarding steps are the only animated forms.
- **shadcn/ui:** not introduced. `packages/ui` already wraps Radix in the Mise styles.

## Database (migration `00005_customer_platform.sql`, additive)

| Change | What |
|---|---|
| `customers` + columns | `phone`, `contact_channel`, `language`, `delivery_country`, `delivery_region` |
| `customer_households` | `customer_id` PK, `name`, `size`, `adults`, `children`, timestamps |
| `customer_preferences` | `customer_id` PK, `dietary`, `meal_interests`, `proteins`, `max_cook_minutes`, `other_restrictions`, `source`, `allergies_reviewed_at`, timestamps |
| `customer_allergies` | (`customer_id`, `allergen`) PK, `detail` (only for `other`), `created_at` |
| `customer_addresses` | Multiple per customer; one default (partial unique index); TT regions |
| `onboarding_progress` | `customer_id` PK, `step`, `skipped`, `completed_at`. The answers themselves go straight into the tables above |
| `notification_preferences` | (`customer_id`, `topic`) PK, `enabled`, `source`, `updated_at` |
| `chef_profiles`, `meal_categories`, `meals`, `meal_images`, `meal_ingredients`, `meal_nutrition` | Catalogue; `status` (`draft`, `published`, `unpublished`); `slug` unique; `is_demo`; `version` for safe concurrent edits |
| `customer_favorites` | (`customer_id`, `meal_id`) PK |
| `delivery_zones` | Empty; for later |
| `auth_tokens` | `purpose` gains `change_email`; new nullable `new_email` column |
| Permissions | `catalog.read`, `catalog.manage`, `catalog.publish`, `nutrition.verify` |

## Backend modules

- **`internal/customers`:** extended with the profile, household, preferences and
  allergies, onboarding, and notification preferences (separate files per concern, sqlc
  queries).
- **`internal/addresses`:** addresses, plus the delivery-zone `Eligibility` interface.
- **`internal/catalog`:** public reads, filtering, search, admin writes and publishing.
- **`internal/favorites`.**
- **`internal/identity`:** gains email change and password change.

## API (all in `docs/api/openapi.yaml`)

- **Customer:**
  - `PATCH /me`
  - `PUT /me/password`
  - `POST /me/email`, `POST /auth/confirm-email-change`
  - `GET|PUT /me/preferences`
  - `GET|PUT /me/household`
  - `GET|POST /me/addresses`, `PATCH|DELETE /me/addresses/{id}`
  - `GET|POST /me/favorites`, `DELETE /me/favorites/{mealId}`
  - `GET|PUT /me/onboarding`
  - `GET|PUT /me/notification-preferences`
  - `GET /me/recommendations`
- **Public catalogue:**
  - `GET /meals`, `GET /meals/{slug}`
  - `GET /meal-categories`
  - `GET /chefs/{slug}`
- **Admin:**
  - `GET|POST /admin/catalog/meals`
  - `GET|PUT /admin/catalog/meals/{id}`
  - `POST /admin/catalog/meals/{id}/publish|unpublish`
  - `PUT /admin/catalog/meals/{id}/nutrition`
  - `GET|POST /admin/catalog/categories`
  - `GET|POST /admin/catalog/chefs`, `PUT /admin/catalog/chefs/{id}`

## Customer app routes (`apps/web`)

- **Signed out:** `/sign-in` (with `/login` redirecting to it), `/register`,
  `/forgot-password`, `/reset-password`, `/verify-email`, `/confirm-email-change`.
- **Onboarding:** `/onboarding`, seven steps, resumable.
- **Signed in:** `/dashboard`, `/meals`, `/meals/[slug]`, `/favorites`, `/profile`, and
  under it `preferences`, `household`, `addresses`, `notifications` and `security`.
- **Future destinations:** subscription, orders, deliveries, billing and rewards are
  **not** shown in navigation. They return 404 until they are built.

## Tests

- **Go integration tests against PostgreSQL** for every new endpoint:
  - ownership: another customer's address or favourite returns 404
  - drafts and demo content are hidden
  - filters and search
  - validation and contradictory preferences
  - allergies never overwritten
  - the waitlist import only when the customer's own records are empty, idempotent
  - email and password change
  - permissions on the admin catalogue
- **Vitest:** greeting time zone, onboarding step logic, filter query building.
- **Playwright:**
  - onboarding: resume after reload, then complete
  - dashboard
  - browse, filter, view a meal and save it as a favourite
  - profile, addresses and default address, notifications, security
  - admin: create and publish a meal
  - axe scans
  - the existing 24 tests as the regression check
- **Screenshots** of the main pages at 1440 px and 390 px.

## Progress

**Complete** (11 November 2026). Everything above is built and verified. The results are in
`docs/SPRINT_2_REPORT.md`. Not committed yet.
