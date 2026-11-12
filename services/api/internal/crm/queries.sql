-- Queries for the CRM module (sqlc): lead stages and assignees, notes,
-- segments and campaigns. Leads and customers themselves are not here.

-- name: GetLeadProfile :one
SELECT * FROM crm_lead_profiles WHERE lead_id = @lead_id;

-- name: UpsertLeadProfile :one
INSERT INTO crm_lead_profiles (lead_id, stage, assigned_to, updated_by, stage_changed_at)
VALUES (@lead_id, @stage, sqlc.narg(assigned_to), sqlc.narg(updated_by), now())
ON CONFLICT (lead_id) DO UPDATE SET
    stage_changed_at = CASE WHEN crm_lead_profiles.stage <> excluded.stage THEN now() ELSE crm_lead_profiles.stage_changed_at END,
    stage = excluded.stage, assigned_to = excluded.assigned_to, updated_by = excluded.updated_by, updated_at = now()
RETURNING *;

-- name: LeadExists :one
SELECT EXISTS (SELECT 1 FROM leads WHERE id = @id);

-- name: CustomerExists :one
SELECT EXISTS (SELECT 1 FROM customers WHERE id = @id);

-- name: StaffUserExists :one
SELECT EXISTS (SELECT 1 FROM users u JOIN user_roles r ON r.user_id = u.id AND r.scope_type IS NULL AND r.role_key <> 'customer'
               WHERE u.id = @id AND u.status = 'active');

-- name: AddLeadNote :one
INSERT INTO crm_notes (lead_id, body, author_id) VALUES (@lead_id, @body, @author_id) RETURNING *;

-- name: AddCustomerNote :one
INSERT INTO crm_notes (customer_id, body, author_id) VALUES (@customer_id, @body, @author_id) RETURNING *;

-- name: LeadNotes :many
SELECT n.*, coalesce(trim(u.first_name || ' ' || u.last_name), '')::text AS author_name
FROM crm_notes n LEFT JOIN users u ON u.id = n.author_id
WHERE n.lead_id = @lead_id ORDER BY n.created_at DESC LIMIT 200;

-- name: CustomerNotes :many
SELECT n.*, coalesce(trim(u.first_name || ' ' || u.last_name), '')::text AS author_name
FROM crm_notes n LEFT JOIN users u ON u.id = n.author_id
WHERE n.customer_id = @customer_id ORDER BY n.created_at DESC LIMIT 200;

-- ---------------------------------------------------------------- segments

-- name: ListSegments :many
SELECT * FROM crm_segments ORDER BY name;

-- name: GetSegment :one
SELECT * FROM crm_segments WHERE id = @id;

-- name: CreateSegment :one
INSERT INTO crm_segments (name, description, rules, created_by, updated_by)
VALUES (@name, @description, @rules, @actor, @actor) RETURNING *;

-- name: UpdateSegment :one
UPDATE crm_segments SET name = @name, description = @description, rules = @rules, updated_by = @actor, updated_at = now()
WHERE id = @id RETURNING *;

-- name: DeleteSegment :execrows
DELETE FROM crm_segments WHERE id = @id;

-- ---------------------------------------------------------------- campaigns

-- name: ListCampaigns :many
SELECT * FROM marketing_campaigns ORDER BY coalesce(starts_at, created_at) DESC LIMIT 200;

-- name: GetCampaign :one
SELECT * FROM marketing_campaigns WHERE id = @id;

-- name: CreateCampaign :one
INSERT INTO marketing_campaigns (name, objective, channel, utm_campaign, segment_id, promotion_id, starts_at, ends_at, notes, created_by, updated_by)
VALUES (@name, @objective, @channel, sqlc.narg(utm_campaign), sqlc.narg(segment_id), sqlc.narg(promotion_id),
        sqlc.narg(starts_at), sqlc.narg(ends_at), @notes, @actor, @actor)
RETURNING *;

-- name: UpdateCampaign :one
UPDATE marketing_campaigns SET name = @name, objective = @objective, channel = @channel, utm_campaign = sqlc.narg(utm_campaign),
    segment_id = sqlc.narg(segment_id), promotion_id = sqlc.narg(promotion_id), starts_at = sqlc.narg(starts_at),
    ends_at = sqlc.narg(ends_at), notes = @notes, updated_by = @actor, updated_at = now()
WHERE id = @id RETURNING *;

-- name: SetCampaignStatus :one
UPDATE marketing_campaigns SET status = @status, updated_by = @actor, updated_at = now() WHERE id = @id RETURNING *;
