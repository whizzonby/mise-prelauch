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

-- name: GetByLeadID :one
SELECT * FROM customers WHERE lead_id = @lead_id;
