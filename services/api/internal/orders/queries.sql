-- Queries for orders (sqlc). Orders and their items are snapshots: once
-- written they are not edited (order_items has an immutability trigger).

-- name: NextOrderNumber :one
INSERT INTO order_counters (year, last) VALUES (@year, 1)
ON CONFLICT (year) DO UPDATE SET last = order_counters.last + 1
RETURNING last;

-- name: CreateOrder :one
INSERT INTO orders (number, customer_id, subscription_id, subscription_cycle_id, billing_cycle_id, payment_id, menu_id,
                    week_start, status, currency, subtotal_minor, discount_minor, delivery_fee_minor, tax_minor, total_minor,
                    meals_count, plan_name, address_snapshot, customer_allergens)
VALUES (@number, @customer_id, @subscription_id, @subscription_cycle_id, @billing_cycle_id, @payment_id, @menu_id,
        @week_start, 'confirmed', @currency, @subtotal_minor, @discount_minor, @delivery_fee_minor, @tax_minor, @total_minor,
        @meals_count, @plan_name, @address_snapshot, @customer_allergens)
ON CONFLICT (subscription_cycle_id) DO NOTHING
RETURNING *;

-- name: OrderForCycle :one
SELECT * FROM orders WHERE subscription_cycle_id = @subscription_cycle_id;

-- name: AddOrderItem :exec
INSERT INTO order_items (order_id, meal_id, meal_version, meal_name, meal_slug, quantity, servings, allergens, allergen_status, dietary_tags)
VALUES (@order_id, @meal_id, @meal_version, @meal_name, @meal_slug, @quantity, @servings, @allergens, @allergen_status, @dietary_tags);

-- name: AddOrderStatus :exec
INSERT INTO order_status_history (order_id, from_status, to_status, reason, actor_user_id)
VALUES (@order_id, sqlc.narg(from_status), @to_status, sqlc.narg(reason), sqlc.narg(actor));

-- name: GetOrder :one
SELECT * FROM orders WHERE id = @id;

-- name: GetOrderForCustomer :one
SELECT * FROM orders WHERE id = @id AND customer_id = @customer_id;

-- name: OrderItems :many
SELECT * FROM order_items WHERE order_id = @order_id ORDER BY meal_name;

-- name: OrderHistory :many
SELECT * FROM order_status_history WHERE order_id = @order_id ORDER BY id;

-- name: ListOrdersForCustomer :many
SELECT * FROM orders WHERE customer_id = @customer_id ORDER BY week_start DESC, created_at DESC LIMIT @lim OFFSET @off;

-- name: ListOrders :many
SELECT * FROM orders
WHERE (sqlc.narg(week_start)::date IS NULL OR week_start = sqlc.narg(week_start)::date)
  AND (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status)::text)
  AND (sqlc.narg(customer_id)::uuid IS NULL OR customer_id = sqlc.narg(customer_id)::uuid)
ORDER BY week_start DESC, number DESC
LIMIT 200;

-- name: Demand :many
-- Finalised demand for a week, per meal version: what later production
-- modules will read. Cancelled orders are left out.
SELECT i.meal_id, i.meal_version, i.meal_name, i.servings,
       sum(i.quantity)::int AS quantity,
       count(DISTINCT o.id)::int AS orders,
       array_agg(DISTINCT o.number ORDER BY o.number)::text[] AS order_numbers
FROM order_items i JOIN orders o ON o.id = i.order_id
WHERE o.week_start = @week_start AND o.status <> 'cancelled'
GROUP BY i.meal_id, i.meal_version, i.meal_name, i.servings
ORDER BY i.meal_name, i.meal_version;

-- name: DemandAllergens :many
-- For each meal of the week, the allergy keys declared by the customers who
-- ordered it (keys only, no names), with how many orders carry each.
SELECT i.meal_id, a.allergen::text AS allergen, count(*)::int AS orders
FROM order_items i JOIN orders o ON o.id = i.order_id
CROSS JOIN LATERAL unnest(o.customer_allergens) AS a(allergen)
WHERE o.week_start = @week_start AND o.status <> 'cancelled'
GROUP BY i.meal_id, a.allergen
ORDER BY i.meal_id, a.allergen;
