-- Queries for internal support cases (sqlc).

-- name: CreateCase :one
INSERT INTO support_cases (customer_id, lead_id, subject, category, priority, assigned_to, created_by)
VALUES (sqlc.narg(customer_id), sqlc.narg(lead_id), @subject, @category, @priority, sqlc.narg(assigned_to), @created_by)
RETURNING *;

-- name: GetCase :one
SELECT sqlc.embed(c), coalesce(trim(u.first_name || ' ' || u.last_name), '')::text AS assignee_name
FROM support_cases c LEFT JOIN users u ON u.id = c.assigned_to
WHERE c.id = @id;

-- name: GetCaseForUpdate :one
SELECT * FROM support_cases WHERE id = @id FOR UPDATE;

-- name: UpdateCase :one
UPDATE support_cases SET status = @status, priority = @priority, assigned_to = sqlc.narg(assigned_to), resolution = sqlc.narg(resolution),
    resolved_at = CASE WHEN @status::text IN ('resolved', 'closed') THEN coalesce(resolved_at, now()) ELSE NULL END,
    closed_at = CASE WHEN @status::text = 'closed' THEN coalesce(closed_at, now()) ELSE NULL END,
    updated_at = now()
WHERE id = @id RETURNING *;

-- name: TouchCase :exec
UPDATE support_cases SET updated_at = now() WHERE id = @id;

-- name: AddCaseEvent :exec
INSERT INTO support_case_events (case_id, type, body, from_value, to_value, actor_id)
VALUES (@case_id, @type, sqlc.narg(body), sqlc.narg(from_value), sqlc.narg(to_value), sqlc.narg(actor_id));

-- name: CaseEvents :many
SELECT e.*, coalesce(trim(u.first_name || ' ' || u.last_name), '')::text AS actor_name
FROM support_case_events e LEFT JOIN users u ON u.id = e.actor_id
WHERE e.case_id = @case_id ORDER BY e.id;

-- name: ListCases :many
SELECT c.*, coalesce(trim(u.first_name || ' ' || u.last_name), '')::text AS assignee_name,
       count(*) OVER ()::int AS total
FROM support_cases c LEFT JOIN users u ON u.id = c.assigned_to
WHERE (sqlc.narg(status)::text IS NULL OR c.status = sqlc.narg(status)::text)
  AND (NOT @open_only::bool OR c.status IN ('open', 'in_progress', 'waiting_for_customer'))
  AND (sqlc.narg(customer_id)::uuid IS NULL OR c.customer_id = sqlc.narg(customer_id)::uuid)
  AND (sqlc.narg(assigned_to)::uuid IS NULL OR c.assigned_to = sqlc.narg(assigned_to)::uuid)
  AND (sqlc.narg(priority)::text IS NULL OR c.priority = sqlc.narg(priority)::text)
ORDER BY CASE c.priority WHEN 'urgent' THEN 0 WHEN 'high' THEN 1 WHEN 'normal' THEN 2 ELSE 3 END, c.created_at DESC
LIMIT @lim OFFSET @off;

-- name: StaleOpenCases :many
SELECT id, number, subject, customer_id FROM support_cases
WHERE status IN ('open', 'in_progress') AND updated_at < @before::timestamptz ORDER BY updated_at LIMIT 100;

-- name: CustomerExists :one
SELECT EXISTS (SELECT 1 FROM customers WHERE id = @id);

-- name: LeadExists :one
SELECT EXISTS (SELECT 1 FROM leads WHERE id = @id);

-- name: StaffUserExists :one
SELECT EXISTS (SELECT 1 FROM users u JOIN user_roles r ON r.user_id = u.id AND r.scope_type IS NULL AND r.role_key <> 'customer'
               WHERE u.id = @id AND u.status = 'active');
