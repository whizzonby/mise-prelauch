-- Queries for the customers module (sqlc).

-- name: CreateCustomer :one
INSERT INTO customers (user_id, terms_version, terms_accepted_at)
VALUES (@user_id, @terms_version, now())
RETURNING *;

-- name: LinkLead :execrows
-- Links only an unlinked customer to a lead no other customer holds.
UPDATE customers AS cu SET lead_id = @lead_id, lead_linked_at = now(), updated_at = now()
WHERE cu.user_id = @user_id
  AND cu.lead_id IS NULL
  AND NOT EXISTS (SELECT 1 FROM customers c WHERE c.lead_id = @lead_id);

-- name: GetByUserID :one
SELECT * FROM customers WHERE user_id = @user_id;

-- name: GetByID :one
SELECT * FROM customers WHERE id = @id;

-- name: GetByLeadID :one
SELECT * FROM customers WHERE lead_id = @lead_id;

-- ---------------------------------------------------------------- profile

-- name: UpdateProfile :one
UPDATE customers SET
    phone = sqlc.narg(phone),
    contact_channel = @contact_channel,
    language = @language,
    delivery_country = sqlc.narg(delivery_country),
    delivery_region = sqlc.narg(delivery_region),
    updated_at = now()
WHERE id = @id
RETURNING *;

-- ---------------------------------------------------------------- household

-- name: GetHousehold :one
SELECT * FROM customer_households WHERE customer_id = @customer_id;

-- name: UpsertHousehold :one
INSERT INTO customer_households (customer_id, name, size, adults, children, source)
VALUES (@customer_id, sqlc.narg(name), @size, sqlc.narg(adults), sqlc.narg(children), @source)
ON CONFLICT (customer_id) DO UPDATE SET
    name = EXCLUDED.name, size = EXCLUDED.size, adults = EXCLUDED.adults,
    children = EXCLUDED.children, source = EXCLUDED.source, updated_at = now()
RETURNING *;

-- name: InsertHouseholdIfMissing :execrows
INSERT INTO customer_households (customer_id, size, source)
VALUES (@customer_id, @size, 'waitlist')
ON CONFLICT (customer_id) DO NOTHING;

-- ---------------------------------------------------------------- preferences

-- name: GetPreferences :one
SELECT * FROM customer_preferences WHERE customer_id = @customer_id;

-- name: UpsertPreferences :one
INSERT INTO customer_preferences (customer_id, dietary, meal_interests, proteins, max_cook_minutes, other_restrictions, source, allergies_reviewed_at)
VALUES (@customer_id, @dietary, @meal_interests, @proteins, sqlc.narg(max_cook_minutes), sqlc.narg(other_restrictions), @source, sqlc.narg(allergies_reviewed_at))
ON CONFLICT (customer_id) DO UPDATE SET
    dietary = EXCLUDED.dietary, meal_interests = EXCLUDED.meal_interests, proteins = EXCLUDED.proteins,
    max_cook_minutes = EXCLUDED.max_cook_minutes, other_restrictions = EXCLUDED.other_restrictions,
    source = EXCLUDED.source, allergies_reviewed_at = EXCLUDED.allergies_reviewed_at, updated_at = now()
RETURNING *;

-- name: InsertWaitlistPreferencesIfMissing :execrows
-- Copies waitlist answers only into a customer who has no preferences yet.
INSERT INTO customer_preferences (customer_id, dietary, meal_interests, source)
VALUES (@customer_id, @dietary, @meal_interests, 'waitlist')
ON CONFLICT (customer_id) DO NOTHING;

-- name: ListAllergies :many
SELECT * FROM customer_allergies WHERE customer_id = @customer_id ORDER BY allergen;

-- name: DeleteAllergies :exec
DELETE FROM customer_allergies WHERE customer_id = @customer_id;

-- name: InsertAllergy :exec
INSERT INTO customer_allergies (customer_id, allergen, detail) VALUES (@customer_id, @allergen, sqlc.narg(detail));

-- ---------------------------------------------------------------- onboarding

-- name: GetOnboarding :one
SELECT * FROM onboarding_progress WHERE customer_id = @customer_id;

-- name: UpsertOnboarding :one
INSERT INTO onboarding_progress (customer_id, step, skipped, completed_at)
VALUES (@customer_id, @step, @skipped, sqlc.narg(completed_at))
ON CONFLICT (customer_id) DO UPDATE SET
    step = EXCLUDED.step, skipped = EXCLUDED.skipped,
    -- Completing is remembered: going back over a step later keeps the date.
    completed_at = COALESCE(onboarding_progress.completed_at, EXCLUDED.completed_at),
    updated_at = now()
RETURNING *;

-- ---------------------------------------------------------------- notifications

-- name: ListNotificationPreferences :many
SELECT * FROM notification_preferences WHERE customer_id = @customer_id;

-- name: SetNotificationPreference :exec
INSERT INTO notification_preferences (customer_id, topic, enabled, source)
VALUES (@customer_id, @topic, @enabled, 'customer')
ON CONFLICT (customer_id, topic) DO UPDATE SET
    enabled = EXCLUDED.enabled, source = 'customer', updated_at = now()
WHERE notification_preferences.enabled IS DISTINCT FROM EXCLUDED.enabled;

-- name: InsertConsentTopicIfMissing :execrows
INSERT INTO notification_preferences (customer_id, topic, enabled, source)
VALUES (@customer_id, @topic, true, 'waitlist_consent')
ON CONFLICT (customer_id, topic) DO NOTHING;

