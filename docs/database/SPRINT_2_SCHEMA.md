# Sprint 2 physical schema

Migration `services/api/migrations/00005_customer_platform.sql`. It is additive.
Leads, referrals, attribution and lead events are not touched.

## Changes to existing tables

- **`customers`:** five new columns.
  - `phone`
  - `contact_channel` (`email`, `sms` or `whatsapp`)
  - `language` (`en`)
  - `delivery_country`, `delivery_region`: where the customer expects deliveries. This
    is not a promise of delivery.
- **`auth_tokens`:** a new purpose, `change_email`, and a `new_email` column (set only
  for that purpose).
- **`sessions`:** a new revoke reason, `password_changed`.
- **`permissions`:** four new permissions: `catalog.read`, `catalog.manage`,
  `catalog.publish` and `nutrition.verify`.
  - super_admin and admin get the three catalogue permissions.
  - marketing gets read and manage.
  - nutritionist gets admin.access, catalog.read and nutrition.verify.

## The customer's own records

| Table | Key | Notes |
|---|---|---|
| `customer_households` | `customer_id` | `size` 1–20; `adults` and `children` optional; their total is at most `size`. Counts only. `source` is `customer` or `waitlist` |
| `customer_preferences` | `customer_id` | `dietary`, `meal_interests` and `proteins` (text arrays, keys from `internal/vocab`), `max_cook_minutes`, `other_restrictions`, `source`, `allergies_reviewed_at` (null means never answered) |
| `customer_allergies` | (`customer_id`, `allergen`) | Kept apart from preferences. `detail` is required for `other` and allowed only for it |
| `customer_addresses` | `id` | Many per customer (at most 10, enforced in Go). One default, enforced by a partial unique index. Every read and write is keyed by `customer_id` as well as `id` |
| `delivery_zones` | `id`, unique (`country`, `region`) | Empty. While it is empty, every place is `unknown` |
| `onboarding_progress` | `customer_id` | `step`, `skipped`, `completed_at`. Answers are stored once, in the tables above |
| `notification_preferences` | (`customer_id`, `topic`) | Optional topics only. `source` is `customer` or `waitlist_consent`; changes are audited as consent records |
| `customer_favorites` | (`customer_id`, `meal_id`) | No duplicates; cascades with either side |

## Catalogue

| Table | Notes |
|---|---|
| `meal_categories` | `slug` unique, `position`, `is_demo` |
| `chef_profiles` | `slug` unique; `status` (`draft`, `published`, `unpublished`); `is_demo` |
| `meals` | `slug` unique, `summary`, `description`, `cooking_overview`, prep and cook minutes, `servings`, `protein`, `dietary_tags[]` (GIN index), `allergens[]` with `allergen_status` (`unconfirmed` or `declared`), `status` and `published_at`, `is_demo`, `version` (optimistic concurrency), `created_by` and `updated_by`. Full-text search uses an expression GIN index |
| `meal_images` | Ordered by `position`; position 0 is the main photograph. `alt` is required; width, height and credit are optional |
| `meal_ingredients` | Display text (name and quantity) for the ingredients in the kit. Not production quantities |
| `meal_nutrition` | Per serving. `status` is `pending` or `verified`; `verified_by` and `verified_at`. Only verified figures leave the API |

## Rollback

`goose down` on 00005 drops the new tables and columns, and restores the previous
constraints. Sessions revoked as `password_changed` become `user_revoked`;
open `change_email` tokens are deleted. A rollback is tested from 5 to 3 and back up
again in `TestMigrationCopiesPrelaunchAdmins`.

## Migration 00006: delivery zones

`00006_delivery_zones_tt.sql` makes every Trinidad and Tobago region an active delivery zone.
This was a business decision on 11 November 2026. Its rollback removes those 15 rows.
