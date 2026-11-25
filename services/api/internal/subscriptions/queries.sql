-- Queries for subscriptions, their weekly cycles and meal selections (sqlc).

-- ---------------------------------------------------------------- subscriptions

-- name: CreateSubscription :one
INSERT INTO subscriptions (customer_id, plan_id, plan_price_id, meals_per_week, amount_minor, currency,
                           delivery_address_id, first_week)
VALUES (@customer_id, @plan_id, @plan_price_id, @meals_per_week, @amount_minor, @currency,
        @delivery_address_id, @first_week)
RETURNING *;

-- name: GetSubscription :one
SELECT * FROM subscriptions WHERE id = @id;

-- name: GetSubscriptionForUpdate :one
SELECT * FROM subscriptions WHERE id = @id FOR UPDATE;

-- name: LiveSubscription :one
SELECT * FROM subscriptions WHERE customer_id = @customer_id AND status NOT IN ('cancelled', 'expired');

-- name: LatestSubscription :one
SELECT * FROM subscriptions WHERE customer_id = @customer_id ORDER BY created_at DESC LIMIT 1;

-- name: SetSubscriptionStatus :one
UPDATE subscriptions SET status = @status,
    activated_at = CASE WHEN @status::text = 'active' THEN coalesce(activated_at, @now::timestamptz) ELSE activated_at END,
    cancelled_at = CASE WHEN @status::text = 'cancelled' THEN @now::timestamptz ELSE cancelled_at END,
    expired_at = CASE WHEN @status::text = 'expired' THEN @now::timestamptz ELSE expired_at END,
    version = version + 1, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: SetSubscriptionPlan :exec
UPDATE subscriptions SET plan_id = @plan_id, plan_price_id = @plan_price_id, meals_per_week = @meals_per_week,
    amount_minor = @amount_minor, currency = @currency, version = version + 1, updated_at = now()
WHERE id = @id;

-- name: SetSubscriptionPause :exec
UPDATE subscriptions SET pause_from_week = sqlc.narg(pause_from_week), pause_until_week = sqlc.narg(pause_until_week),
    version = version + 1, updated_at = now()
WHERE id = @id;

-- name: SetSubscriptionCancel :exec
UPDATE subscriptions SET cancel_requested_at = sqlc.narg(requested_at), cancel_effective_week = sqlc.narg(effective_week),
    cancel_reason = sqlc.narg(reason), version = version + 1, updated_at = now()
WHERE id = @id;

-- name: SetSubscriptionAddress :exec
UPDATE subscriptions SET delivery_address_id = @address_id, version = version + 1, updated_at = now() WHERE id = @id;

-- name: SetSubscriptionPaymentMethod :exec
UPDATE subscriptions SET payment_method_id = @payment_method_id, updated_at = now() WHERE id = @id;

-- name: AddressInUse :one
SELECT EXISTS (SELECT 1 FROM subscriptions WHERE delivery_address_id = @address_id AND status NOT IN ('cancelled', 'expired'));

-- name: ListSubscriptions :many
SELECT s.*, p.name AS plan_name
FROM subscriptions s JOIN subscription_plans p ON p.id = s.plan_id
WHERE (sqlc.narg(status)::text IS NULL OR s.status = sqlc.narg(status)::text)
  AND (sqlc.narg(customer_id)::uuid IS NULL OR s.customer_id = sqlc.narg(customer_id)::uuid)
ORDER BY s.created_at DESC
LIMIT @lim OFFSET @off;

-- name: SubscriptionsNeedingCycles :many
-- Subscriptions that may need their next week created.
SELECT id FROM subscriptions
WHERE status IN ('active', 'pause_scheduled', 'paused', 'past_due', 'cancel_scheduled')
ORDER BY id;

-- name: StalePendingSubscriptions :many
SELECT id FROM subscriptions WHERE status = 'pending_payment' AND created_at <= @before::timestamptz ORDER BY created_at LIMIT 100;

-- name: AddEvent :one
INSERT INTO subscription_events (subscription_id, type, from_status, to_status, effective_week, actor_user_id, metadata)
VALUES (@subscription_id, @type, sqlc.narg(from_status), sqlc.narg(to_status), sqlc.narg(effective_week), sqlc.narg(actor), @metadata)
RETURNING id;

-- name: ListEvents :many
SELECT * FROM subscription_events WHERE subscription_id = @subscription_id ORDER BY id DESC LIMIT 100;

-- name: AddChange :exec
INSERT INTO subscription_changes (subscription_id, from_plan_price_id, to_plan_price_id, effective_week, requested_by)
VALUES (@subscription_id, @from_plan_price_id, @to_plan_price_id, @effective_week, sqlc.narg(requested_by));

-- name: AddPause :exec
INSERT INTO subscription_pauses (subscription_id, from_week, until_week, status, requested_by)
VALUES (@subscription_id, @from_week, @until_week, @status, sqlc.narg(requested_by));

-- name: EndPauses :exec
UPDATE subscription_pauses SET status = @status, updated_at = now()
WHERE subscription_id = @subscription_id AND status IN ('scheduled', 'active');

-- name: ActivatePauses :exec
UPDATE subscription_pauses SET status = 'active', updated_at = now()
WHERE subscription_id = @subscription_id AND status = 'scheduled';

-- ---------------------------------------------------------------- cycles

-- name: InsertCycle :one
INSERT INTO subscription_cycles (subscription_id, customer_id, week_start, cutoff_at, renewal_at, plan_id, plan_price_id,
                                 meals_per_week, amount_minor, currency, state, state_reason)
VALUES (@subscription_id, @customer_id, @week_start, @cutoff_at, @renewal_at, @plan_id, @plan_price_id,
        @meals_per_week, @amount_minor, @currency, @state, sqlc.narg(state_reason))
ON CONFLICT (subscription_id, week_start) DO NOTHING
RETURNING *;

-- name: GetCycle :one
SELECT * FROM subscription_cycles WHERE id = @id;

-- name: GetCycleForUpdate :one
SELECT * FROM subscription_cycles WHERE id = @id FOR UPDATE;

-- name: CycleForWeek :one
SELECT * FROM subscription_cycles WHERE subscription_id = @subscription_id AND week_start = @week_start;

-- name: LatestCycle :one
SELECT * FROM subscription_cycles WHERE subscription_id = @subscription_id ORDER BY week_start DESC LIMIT 1;

-- name: ScheduledCycles :many
SELECT * FROM subscription_cycles WHERE subscription_id = @subscription_id AND state = 'scheduled' ORDER BY week_start;

-- name: SetCycleState :one
UPDATE subscription_cycles SET state = @state, state_reason = sqlc.narg(state_reason),
    finalized_at = CASE WHEN @state::text IN ('finalized', 'unpaid', 'needs_resolution') THEN @now::timestamptz ELSE finalized_at END,
    updated_at = now()
WHERE id = @id
RETURNING *;

-- name: SetCyclePlan :exec
UPDATE subscription_cycles SET plan_id = @plan_id, plan_price_id = @plan_price_id, meals_per_week = @meals_per_week,
    amount_minor = @amount_minor, currency = @currency, updated_at = now()
WHERE id = @id AND state = 'scheduled';

-- name: CyclesPastCutoff :many
SELECT id FROM subscription_cycles WHERE state = 'scheduled' AND cutoff_at <= @now::timestamptz ORDER BY cutoff_at LIMIT 200;

-- name: CyclesDueForRenewal :many
SELECT id FROM subscription_cycles WHERE state = 'scheduled' AND renewal_at <= @now::timestamptz AND cutoff_at > @now::timestamptz
ORDER BY renewal_at LIMIT 200;

-- name: CyclesForCustomer :many
SELECT c.*, p.name AS plan_name
FROM subscription_cycles c JOIN subscription_plans p ON p.id = c.plan_id
WHERE c.customer_id = @customer_id
ORDER BY c.week_start DESC
LIMIT @lim;

-- name: CyclesForSubscription :many
SELECT c.*, p.name AS plan_name
FROM subscription_cycles c JOIN subscription_plans p ON p.id = c.plan_id
WHERE c.subscription_id = @subscription_id
ORDER BY c.week_start DESC
LIMIT 104;

-- name: CycleForCustomer :one
SELECT * FROM subscription_cycles WHERE id = @id AND customer_id = @customer_id;

-- name: CyclePlanName :one
SELECT p.name FROM subscription_cycles c JOIN subscription_plans p ON p.id = c.plan_id WHERE c.id = @id;

-- name: CustomersWithCycleInWeek :many
-- Subscribers who will receive a week and have not confirmed their meals.
SELECT c.id AS cycle_id, c.customer_id, c.subscription_id, c.cutoff_at
FROM subscription_cycles c
LEFT JOIN meal_selections s ON s.subscription_cycle_id = c.id
WHERE c.week_start = @week_start AND c.state = 'scheduled'
  AND (NOT @unconfirmed_only::bool OR s.id IS NULL OR s.status = 'draft');

-- name: CyclesNearingCutoff :many
SELECT c.id AS cycle_id, c.customer_id, c.week_start, c.cutoff_at
FROM subscription_cycles c
LEFT JOIN meal_selections s ON s.subscription_cycle_id = c.id
WHERE c.state = 'scheduled' AND c.cutoff_at > @now::timestamptz AND c.cutoff_at <= @until::timestamptz
  AND (s.id IS NULL OR s.status = 'draft');

-- ---------------------------------------------------------------- selections

-- name: GetSelectionForCycle :one
SELECT * FROM meal_selections WHERE subscription_cycle_id = @subscription_cycle_id;

-- name: CreateSelection :one
INSERT INTO meal_selections (subscription_cycle_id, customer_id, menu_id)
VALUES (@subscription_cycle_id, @customer_id, @menu_id)
ON CONFLICT (subscription_cycle_id) DO NOTHING
RETURNING *;

-- name: SetSelectionStatus :one
UPDATE meal_selections SET status = @status,
    confirmed_at = CASE WHEN @status::text = 'confirmed' THEN @now::timestamptz WHEN @status::text = 'draft' THEN NULL ELSE confirmed_at END,
    locked_at = CASE WHEN @status::text = 'locked' THEN @now::timestamptz ELSE locked_at END,
    was_confirmed = CASE WHEN @status::text = 'locked' THEN (status = 'confirmed') ELSE was_confirmed END,
    version = version + 1, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: ClearSelectionItems :exec
DELETE FROM meal_selection_items WHERE selection_id = @selection_id;

-- name: AddSelectionItem :exec
INSERT INTO meal_selection_items (selection_id, meal_id, quantity) VALUES (@selection_id, @meal_id, @quantity);

-- name: SelectionItems :many
SELECT * FROM meal_selection_items WHERE selection_id = @selection_id ORDER BY meal_id;

-- name: SelectedMealsForMenu :many
SELECT DISTINCT i.meal_id
FROM meal_selection_items i JOIN meal_selections s ON s.id = i.selection_id
WHERE s.menu_id = @menu_id;

-- name: AlignCycleCutoffs :execrows
-- A menu published with its own cutoff (a holiday week, say) moves the
-- cutoff of every week waiting on it. Billing never moves after the cutoff.
UPDATE subscription_cycles SET cutoff_at = @cutoff_at::timestamptz,
    renewal_at = least(renewal_at, @cutoff_at::timestamptz), updated_at = now()
WHERE week_start = @week_start AND state = 'scheduled' AND cutoff_at <> @cutoff_at::timestamptz;
