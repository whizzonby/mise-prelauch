# Database

PostgreSQL 17. The schema is defined by the migrations in `services/api/migrations`, which
are embedded in the binaries and applied with `misectl migrate up`. This document explains
the shape and the reasons; the migrations are the authority.

## Tables

```
leads ──1:1── lead_preferences
  │──1:n── lead_attribution     (at most one "first" and one "latest" per lead)
  │──1:n── lead_events          (also holds anonymous events, lead_id null)
  │──1:n── referrals            (as referrer)
  └──1:1── referrals            (as referred)

jobs                            (background work, not tied to a table)
admin_users ──1:n── admin_sessions
audit_log   ──n:1── admin_users (actor; kept if the admin is deleted)
```

### leads

One row per person.

| Column | Notes |
|---|---|
| `id` | UUID. Stable for the life of the person; becomes the link to their customer account. |
| `first_name` | 1 to 80 characters. |
| `email` | As typed, trimmed and lower-cased. Where mail is sent. |
| `email_canonical` | `email` with any `+tag` removed (and dots, for Gmail). **Unique.** This is what "the same person" means, and it stops one inbox joining or inviting itself repeatedly. |
| `phone` | Optional. Digits with an optional leading `+`. |
| `location` | Option value chosen on the form. |
| `status` | Explicit lifecycle, below. CHECK-constrained. |
| `referral_code` | **Unique.** Seven characters from an alphabet without look-alikes. |
| `referred_by` | The lead who invited them, if any. Cannot be themselves. |
| `email_verified_at` | When they confirmed their address. |
| `consent_at`, `consent_version` | When they ticked the marketing consent box, and which wording they saw. |
| `signup_ip_hash` | Keyed hash of the signup address. Never the address. Used only to flag same-network referrals. |

### Lead lifecycle

`status` is stored, never inferred from other columns, and only named operations change it.

| Status | Meaning | Set by |
|---|---|---|
| `PENDING` | Signed up, email not confirmed. | Signup. |
| `VERIFIED` | On the list and reachable. | The lead confirming their email (or signup, when verification is turned off). |
| `QUALIFIED` | Marked as a strong candidate for the first launch group. | An admin. |
| `CONVERTED` | Became a customer. | The customer migration at launch (not built yet). |
| `UNSUBSCRIBED` | Asked not to be emailed. Kept on record. | The lead, or an admin. Signing up again with consent reverses it. |
| `BLOCKED` | Abuse. Excluded from email, statistics and referral counts. | An admin. |

### lead_preferences

One row per lead: household size and dietary interests from the signup form, plus the
optional profiling answers. `metadata` (JSONB) holds answers that do not yet justify a
column (`household_type`, `fitness_goal`, `usage`).

### referrals

`referrer_lead_id` → `referred_lead_id`, unique on the referred lead (a person is invited
by at most one person), and a CHECK that the two differ.

| Status | Meaning |
|---|---|
| `pending` | The invited lead has not verified. Not counted. |
| `converted` | Verified. Counted. `converted_at` is set (enforced by a CHECK). |
| `flagged` | Signed up from the referrer's own network. Never counted; visible to admins. |

Rewards can be added later as a table keyed on converted referrals without changing this one.

### lead_attribution

UTM parameters, landing page and referring site for the `first` touch (the visit that
first brought the person) and the `latest` touch (the visit they signed up on). Unique on
`(lead_id, touch)`.

### lead_events

First-party analytics events. `lead_id` is null for visitors who have not signed up; at
signup their earlier events (matched by `anonymous_id`) are attached to the new lead.
A CHECK requires at least one of `lead_id` and `anonymous_id`.

### jobs

The background job queue. Rows are claimed with `FOR UPDATE SKIP LOCKED`, retried with
backoff (30 s, 2 min, 8 min, 32 min) and marked `failed` after `max_attempts`. Payloads hold
ids only, never personal data.

### admin_users, admin_sessions

Admin accounts with an Argon2id password hash and a role. Sessions store the SHA-256 of
the bearer token, never the token.

### audit_log

Who did what, when: admin sign-ins, lead status changes, exports (with the filter used and
the row count) and erasures. An erasure removes earlier audit entries that reference the
lead and leaves one record that an erasure happened, with no personal data.

## Indexes

Beyond the unique constraints: `leads(created_at desc)`, `leads(status)`,
`leads(location)`, a partial index on `leads(referred_by)`, a GIN index on
`lead_preferences.dietary_preferences`, `referrals(referrer_lead_id, status)`, a partial
index on first-touch `utm_source`, `lead_events(event_type, created_at desc)` and
`lead_events(lead_id, created_at desc)`, and partial indexes on `jobs` for ready and
running work.

## Deleting and exporting a person's data

- **Erasure**: `DELETE /api/v1/admin/leads/{id}`. Preferences, attribution, events and
  referrals go with the lead through `ON DELETE CASCADE`; queued jobs for the lead are
  removed; leads they invited keep their place with `referred_by` set to null.
- **Export**: the admin lead detail shows everything held about one person; the CSV export
  produces it in bulk.

## Migrations

- Files are `NNNNN_description.sql` with `-- +goose Up` and `-- +goose Down` sections.
- Migrations are forward-only in staging and production. `migrate down` exists for local
  development and is refused elsewhere.
- CI applies every migration to an empty database, rolls the latest back, and re-applies it.
- Deployments run migrations as a one-off task before any service is updated, so a
  migration must work with the previous release still running (add before you remove).

## Backups

RDS automated backups: 7 days in staging, 14 in production, with point-in-time recovery.
Production has deletion protection and takes a final snapshot if it is ever removed.
Restore drills are not automated; see `docs/status.md`.
