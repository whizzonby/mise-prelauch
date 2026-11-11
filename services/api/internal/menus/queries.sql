-- Queries for weekly menus (sqlc).

-- name: ListMenus :many
SELECT m.*, (SELECT count(*) FROM weekly_menu_meals mm WHERE mm.menu_id = m.id)::int AS meal_count
FROM weekly_menus m
WHERE (sqlc.narg(status)::text IS NULL OR m.status = sqlc.narg(status)::text)
  AND (sqlc.narg(from_week)::date IS NULL OR m.week_start >= sqlc.narg(from_week)::date)
ORDER BY m.week_start DESC
LIMIT 60;

-- name: GetMenu :one
SELECT * FROM weekly_menus WHERE id = @id;

-- name: GetMenuForUpdate :one
SELECT * FROM weekly_menus WHERE id = @id FOR UPDATE;

-- name: GetMenuByWeek :one
SELECT * FROM weekly_menus WHERE week_start = @week_start;

-- name: ListPublishedMenus :many
-- What customers may see: published or closed menus from a week onwards.
SELECT * FROM weekly_menus
WHERE status IN ('published', 'closed') AND week_start >= @from_week
ORDER BY week_start
LIMIT 8;

-- name: MenuMeals :many
SELECT * FROM weekly_menu_meals WHERE menu_id = @menu_id ORDER BY position, meal_id;

-- name: CreateMenu :one
INSERT INTO weekly_menus (week_start, title, note, cutoff_at, eligible_plan_ids, created_by, updated_by)
VALUES (@week_start, @title, @note, @cutoff_at, @eligible_plan_ids, @actor, @actor)
RETURNING id;

-- name: UpdateMenu :execrows
UPDATE weekly_menus SET
    title = @title, note = @note, cutoff_at = @cutoff_at, eligible_plan_ids = @eligible_plan_ids,
    version = version + 1, updated_by = @actor, updated_at = now()
WHERE id = @id AND version = @version;

-- name: ClearMenuMeals :exec
DELETE FROM weekly_menu_meals WHERE menu_id = @menu_id;

-- name: AddMenuMeal :exec
INSERT INTO weekly_menu_meals (menu_id, meal_id, position, max_per_customer)
VALUES (@menu_id, @meal_id, @position, @max_per_customer);

-- name: SetMenuStatus :one
UPDATE weekly_menus SET
    status = @status,
    publish_at = CASE WHEN @status::text = 'scheduled' THEN sqlc.narg(publish_at)::timestamptz ELSE NULL END,
    published_at = CASE WHEN @status::text = 'published' THEN coalesce(published_at, @now::timestamptz)
                        WHEN @status::text IN ('draft', 'scheduled') THEN NULL
                        ELSE published_at END,
    version = version + 1, updated_by = sqlc.narg(actor), updated_at = now()
WHERE id = @id
RETURNING *;

-- name: DueScheduledMenus :many
SELECT id FROM weekly_menus WHERE status = 'scheduled' AND publish_at <= @now::timestamptz ORDER BY publish_at FOR UPDATE SKIP LOCKED;

-- name: CloseMenusPastCutoff :many
UPDATE weekly_menus SET status = 'closed', version = version + 1, updated_at = now()
WHERE status = 'published' AND cutoff_at <= @now
RETURNING id;
