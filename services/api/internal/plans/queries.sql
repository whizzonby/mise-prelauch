-- Queries for subscription plans and their price versions (sqlc).
--
-- A plan's current price is its latest price row with effective_from at or
-- before @now. Price rows are never updated; a price change is a new row.

-- name: ListPlansWithPrice :many
-- Every plan with its current price (if any), for the public list and the
-- admin. The caller decides what to show.
SELECT p.*,
       coalesce(cp.id::text, '')::text AS price_id, coalesce(cp.amount_minor, 0)::bigint AS price_amount_minor, coalesce(cp.currency, '')::text AS price_currency,
       coalesce(cp.effective_from, 'epoch')::timestamptz AS price_effective_from,
       coalesce(np.amount_minor, 0)::bigint AS next_amount_minor, coalesce(np.effective_from, 'epoch')::timestamptz AS next_effective_from
FROM subscription_plans p
LEFT JOIN LATERAL (
    SELECT * FROM subscription_plan_prices pp
    WHERE pp.plan_id = p.id AND pp.effective_from <= @now::timestamptz
    ORDER BY pp.effective_from DESC LIMIT 1
) cp ON true
LEFT JOIN LATERAL (
    SELECT * FROM subscription_plan_prices pp
    WHERE pp.plan_id = p.id AND pp.effective_from > @now::timestamptz
    ORDER BY pp.effective_from ASC LIMIT 1
) np ON true
WHERE (sqlc.narg(status)::text IS NULL OR p.status = sqlc.narg(status)::text)
ORDER BY p.sort_order, p.meals_per_week, p.name;

-- name: GetPlanWithPrice :one
SELECT p.*,
       coalesce(cp.id::text, '')::text AS price_id, coalesce(cp.amount_minor, 0)::bigint AS price_amount_minor, coalesce(cp.currency, '')::text AS price_currency,
       coalesce(cp.effective_from, 'epoch')::timestamptz AS price_effective_from,
       coalesce(np.amount_minor, 0)::bigint AS next_amount_minor, coalesce(np.effective_from, 'epoch')::timestamptz AS next_effective_from
FROM subscription_plans p
LEFT JOIN LATERAL (
    SELECT * FROM subscription_plan_prices pp
    WHERE pp.plan_id = p.id AND pp.effective_from <= @now::timestamptz
    ORDER BY pp.effective_from DESC LIMIT 1
) cp ON true
LEFT JOIN LATERAL (
    SELECT * FROM subscription_plan_prices pp
    WHERE pp.plan_id = p.id AND pp.effective_from > @now::timestamptz
    ORDER BY pp.effective_from ASC LIMIT 1
) np ON true
WHERE (sqlc.narg(id)::uuid IS NOT NULL AND p.id = sqlc.narg(id)::uuid)
   OR (sqlc.narg(slug)::text IS NOT NULL AND p.slug = sqlc.narg(slug)::text);

-- name: GetPrice :one
SELECT * FROM subscription_plan_prices WHERE id = @id;

-- name: ListPrices :many
SELECT * FROM subscription_plan_prices WHERE plan_id = @plan_id ORDER BY effective_from DESC;

-- name: CreatePlan :one
INSERT INTO subscription_plans (slug, name, summary, description, meals_per_week, segment, benefits, self_serve,
                                available_from, available_until, sort_order, is_demo, created_by, updated_by)
VALUES (@slug, @name, @summary, @description, @meals_per_week, @segment, @benefits, @self_serve,
        @available_from, @available_until, @sort_order, @is_demo, @actor, @actor)
RETURNING id;

-- name: UpdatePlan :execrows
-- Optimistic concurrency: the update applies only to the version the editor loaded.
UPDATE subscription_plans SET
    name = @name, summary = @summary, description = @description, meals_per_week = @meals_per_week,
    segment = @segment, benefits = @benefits, self_serve = @self_serve,
    available_from = @available_from, available_until = @available_until, sort_order = @sort_order,
    version = version + 1, updated_by = @actor, updated_at = now()
WHERE id = @id AND version = @version;

-- name: SetPlanStatus :execrows
UPDATE subscription_plans SET status = @status, version = version + 1, updated_by = @actor, updated_at = now()
WHERE id = @id;

-- name: InsertPrice :one
INSERT INTO subscription_plan_prices (plan_id, amount_minor, currency, effective_from, note, created_by)
VALUES (@plan_id, @amount_minor, @currency, @effective_from, @note, @actor)
RETURNING *;

-- name: PlanIDBySlug :one
SELECT id FROM subscription_plans WHERE slug = @slug;
