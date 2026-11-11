-- Queries for the addresses module (sqlc). Every statement that reads or
-- changes one address is keyed by both the address id and the customer id, so
-- one customer can never reach another's address by guessing an id.

-- name: ListAddresses :many
SELECT * FROM customer_addresses WHERE customer_id = @customer_id
ORDER BY is_default DESC, created_at;

-- name: GetAddress :one
SELECT * FROM customer_addresses WHERE id = @id AND customer_id = @customer_id;

-- name: CountAddresses :one
SELECT count(*) FROM customer_addresses WHERE customer_id = @customer_id;

-- name: CreateAddress :one
INSERT INTO customer_addresses (customer_id, label, recipient_name, phone, line1, line2, area, city, region, country, instructions, is_default)
VALUES (@customer_id, sqlc.narg(label), @recipient_name, sqlc.narg(phone), @line1, sqlc.narg(line2), sqlc.narg(area), @city, @region, @country, sqlc.narg(instructions), @is_default)
RETURNING *;

-- name: UpdateAddress :one
UPDATE customer_addresses SET
    label = sqlc.narg(label), recipient_name = @recipient_name, phone = sqlc.narg(phone),
    line1 = @line1, line2 = sqlc.narg(line2), area = sqlc.narg(area), city = @city,
    region = @region, country = @country, instructions = sqlc.narg(instructions),
    is_default = @is_default, updated_at = now()
WHERE id = @id AND customer_id = @customer_id
RETURNING *;

-- name: ClearDefault :exec
UPDATE customer_addresses SET is_default = false, updated_at = now()
WHERE customer_id = @customer_id AND is_default AND id <> @keep_id;

-- name: DeleteAddress :one
DELETE FROM customer_addresses WHERE id = @id AND customer_id = @customer_id
RETURNING is_default;

-- name: PromoteOldestToDefault :exec
-- After the default address is deleted, the oldest remaining one takes over.
UPDATE customer_addresses AS a SET is_default = true, updated_at = now()
WHERE a.id = (SELECT o.id FROM customer_addresses o WHERE o.customer_id = @customer_id ORDER BY o.created_at LIMIT 1);

